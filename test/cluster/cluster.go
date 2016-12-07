package cluster

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/test/buildlog"
)

type ClusterType uint8

const (
	ClusterTypeDefault ClusterType = iota
	ClusterTypeRelease
	ClusterTypeNone
)

type BootConfig struct {
	User       string
	Kernel     string
	Network    string
	NatIface   string
	BackupsDir string
}

type Cluster struct {
	ID            string    `json:"id"`
	Instances     instances `json:"instances"`
	ClusterDomain string    `json:"cluster_domain"`
	ControllerPin string    `json:"controller_pin"`
	ControllerKey string    `json:"controller_key"`
	RouterIP      string    `json:"router_ip"`

	defaultInstances []*Instance
	releaseInstances []*Instance

	bc     BootConfig
	vm     *VMManager
	out    io.Writer
	bridge *Bridge
	rootFS string
}

func (c *Cluster) ControllerDomain() string {
	return "controller." + c.ClusterDomain
}

func (c *Cluster) GitDomain() string {
	return "git." + c.ClusterDomain
}

type instances []*Instance

func (i instances) Get(id string) (*Instance, error) {
	for _, inst := range i {
		if inst.ID == id {
			return inst, nil
		}
	}
	return nil, fmt.Errorf("no such host: %s", id)
}

type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func New(bc BootConfig, out io.Writer) *Cluster {
	return &Cluster{
		ID:  random.String(8),
		bc:  bc,
		out: out,
	}
}

func BuildFlynn(bc BootConfig, rootFS, commit string, merge bool, out io.Writer) (string, error) {
	c := New(bc, out)
	defer c.Shutdown()
	return c.BuildFlynn(rootFS, commit, merge, false)
}

func (c *Cluster) log(a ...interface{}) (int, error) {
	return fmt.Fprintln(c.out, append([]interface{}{"++", time.Now().Format("15:04:05.000")}, a...)...)
}

func (c *Cluster) logf(f string, a ...interface{}) (int, error) {
	return fmt.Fprintf(c.out, strings.Join([]string{"++", time.Now().Format("15:04:05.000"), f}, " "), a...)
}

func (c *Cluster) BuildFlynn(rootFS, commit string, merge bool, runTests bool) (string, error) {
	c.log("Building Flynn...")

	if err := c.setup(); err != nil {
		return "", err
	}

	uid, gid, err := lookupUser(c.bc.User)
	if err != nil {
		return "", err
	}

	build, err := c.vm.NewInstance(&VMConfig{
		Kernel: c.bc.Kernel,
		User:   uid,
		Group:  gid,
		Memory: "16384",
		Cores:  8,
		Drives: map[string]*VMDrive{
			"hda": {FS: rootFS, COW: true, Temp: false},
		},
		BackupsDir: c.bc.BackupsDir,
	})
	if err != nil {
		return build.Drive("hda").FS, err
	}
	c.log("Booting build instance...")
	if err := build.Start(); err != nil {
		return build.Drive("hda").FS, fmt.Errorf("error starting build instance: %s", err)
	}

	c.log("Waiting for instance to boot...")
	if err := buildFlynn(build, commit, merge, c.out); err != nil {
		build.Kill()
		return build.Drive("hda").FS, fmt.Errorf("error running build script: %s", err)
	}

	if runTests {
		if err := runUnitTests(build, c.out); err != nil {
			build.Kill()
			return build.Drive("hda").FS, fmt.Errorf("unit tests failed: %s", err)
		}
	}

	if err := build.Shutdown(); err != nil {
		return build.Drive("hda").FS, fmt.Errorf("error while stopping build instance: %s", err)
	}
	c.rootFS = build.Drive("hda").FS
	return c.rootFS, nil
}

type BootResult struct {
	ControllerDomain string
	ControllerPin    string
	ControllerKey    string
	Instances        []*Instance
}

func (c *Cluster) Boot(typ ClusterType, count int, buildLog *buildlog.Log, killOnFailure bool) (res *BootResult, err error) {
	if err := c.setup(); err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			if buildLog != nil && len(c.Instances) > 0 {
				c.DumpLogs(buildLog)
			}
			if killOnFailure {
				c.Shutdown()
			}
		}
	}()

	c.log("Booting", count, "VMs")
	instances, err := c.startVMs(typ, c.rootFS, count, true)
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if err := c.startFlynnHost(inst, instances); err != nil {
			return nil, err
		}
	}

	c.log("Bootstrapping layer 1...")
	if err := c.bootstrapLayer1(instances); err != nil {
		return nil, err
	}

	return &BootResult{
		ControllerDomain: c.ControllerDomain(),
		ControllerPin:    c.ControllerPin,
		ControllerKey:    c.ControllerKey,
		Instances:        instances,
	}, nil
}

func (c *Cluster) BridgeIP() string {
	if c.bridge == nil {
		return ""
	}
	return c.bridge.IP()
}

func (c *Cluster) AddHost() (*Instance, error) {
	if c.rootFS == "" {
		return nil, errors.New("cluster not yet booted")
	}
	c.log("Booting 1 VM")
	instances, err := c.startVMs(ClusterTypeDefault, c.rootFS, 1, false)
	if err != nil {
		return nil, err
	}
	inst := instances[0]
	if err := c.startFlynnHost(inst, c.defaultInstances); err != nil {
		return nil, err
	}
	return inst, err
}

func (c *Cluster) AddVanillaHost(rootFS string) (*Instance, error) {
	c.log("Booting 1 VM")
	instances, err := c.startVMs(ClusterTypeNone, rootFS, 1, false)
	return instances[0], err
}

// RemoveHost stops flynn-host on the instance but leaves it running so the logs
// are still available if we need to dump them later.
func (c *Cluster) RemoveHost(id string) error {
	inst, err := c.Instances.Get(id)
	if err != nil {
		return err
	}
	c.log("removing host", id)

	// ssh into the host and tell the flynn-host daemon to stop
	cmd := "sudo start-stop-daemon --stop --pidfile /var/run/flynn-host.pid --retry 15"
	return inst.Run(cmd, nil)
}

func (c *Cluster) Size() int {
	return len(c.Instances)
}

func (c *Cluster) startVMs(typ ClusterType, rootFS string, count int, initial bool) ([]*Instance, error) {
	uid, gid, err := lookupUser(c.bc.User)
	if err != nil {
		return nil, err
	}

	instances := make([]*Instance, count)
	for i := 0; i < count; i++ {
		memory := "8192"
		if initial && i == 0 {
			// give the first instance more memory as that is where
			// the test binary runs, and the tests use a lot of memory
			memory = "16384"
		}
		inst, err := c.vm.NewInstance(&VMConfig{
			Kernel: c.bc.Kernel,
			User:   uid,
			Group:  gid,
			Memory: memory,
			Cores:  2,
			Drives: map[string]*VMDrive{
				"hda": {FS: rootFS, COW: true, Temp: true},
			},
			BackupsDir: c.bc.BackupsDir,
		})
		if err != nil {
			return nil, fmt.Errorf("error creating instance %d: %s", i, err)
		}
		if err = inst.Start(); err != nil {
			return nil, fmt.Errorf("error starting instance %d: %s", i, err)
		}
		inst.initial = initial
		instances[i] = inst
		c.Instances = append(c.Instances, inst)
		switch typ {
		case ClusterTypeDefault:
			c.defaultInstances = append(c.defaultInstances, inst)
		case ClusterTypeRelease:
			c.releaseInstances = append(c.releaseInstances, inst)
		}
	}
	return instances, nil
}

func (c *Cluster) startFlynnHost(inst *Instance, peerInstances []*Instance) error {
	peers := make([]string, 0, len(peerInstances))
	for _, inst := range peerInstances {
		if !inst.initial {
			continue
		}
		peers = append(peers, inst.IP)
	}
	var script bytes.Buffer
	data := hostScriptData{
		ID:    inst.ID,
		IP:    inst.IP,
		Peers: strings.Join(peers, ","),
	}
	flynnHostScript.Execute(&script, data)
	c.logf("Starting flynn-host on %s [id: %s]\n", inst.IP, inst.ID)
	return inst.Run("bash", &Streams{Stdin: &script, Stdout: c.out, Stderr: os.Stderr})
}

func (c *Cluster) setup() error {
	if _, err := os.Stat(c.bc.Kernel); os.IsNotExist(err) {
		return fmt.Errorf("cluster: not a kernel file: %s", c.bc.Kernel)
	}
	if c.bridge == nil {
		var err error
		name := "flynnbr." + random.String(5)
		c.logf("creating network bridge %s\n", name)
		c.bridge, err = createBridge(name, c.bc.Network, c.bc.NatIface)
		if err != nil {
			return fmt.Errorf("could not create network bridge: %s", err)
		}
	}
	c.vm = NewVMManager(c.bridge)
	return nil
}

func (c *Cluster) Run(command string, s *Streams) error {
	return c.run(command, s, nil)
}

func (c *Cluster) RunWithEnv(command string, s *Streams, env map[string]string) error {
	return c.run(command, s, env)
}

func (c *Cluster) run(command string, s *Streams, env map[string]string) error {
	if len(c.Instances) == 0 {
		return errors.New("no booted servers in cluster")
	}
	return c.Instances[0].RunWithEnv(command, s, env)
}

func (c *Cluster) CLIConfig() (*config.Config, error) {
	conf := &config.Config{}
	s := &config.Cluster{
		Name:          "default",
		ControllerURL: "https://controller." + c.ClusterDomain,
		GitURL:        "https://git." + c.ClusterDomain,
		DockerPushURL: "https://docker." + c.ClusterDomain,
		Key:           c.ControllerKey,
		TLSPin:        c.ControllerPin,
	}
	if err := conf.Add(s, true /*force*/); err != nil {
		return nil, err
	}
	return conf, nil
}

func (c *Cluster) Shutdown() {
	for i, inst := range c.Instances {
		c.logf("killing instance %d [id: %s]\n", i, inst.ID)
		if err := inst.Shutdown(); err != nil {
			c.logf("error killing instance %d: %s\n", i, err)
		}
	}
	if c.bridge != nil {
		c.logf("deleting network bridge %s\n", c.bridge.name)
		if err := deleteBridge(c.bridge); err != nil {
			c.logf("error deleting network bridge %s: %s\n", c.bridge.name, err)
		}
		c.bridge = nil
	}
}

var flynnBuildScript = template.Must(template.New("flynn-build").Parse(`
#!/bin/bash
set -e -x

export GOPATH=~/go
flynn=$GOPATH/src/github.com/flynn/flynn

if [ ! -d $flynn ]; then
  git clone https://github.com/flynn/flynn $flynn
fi

cd $flynn

# Also fetch Github PR commits
if ! git config --get-all remote.origin.fetch | grep -q '^+refs/pull'; then
  git config --add remote.origin.fetch '+refs/pull/*/head:refs/remotes/origin/pr/*'
fi

git fetch
git checkout --quiet {{ .Commit }}

{{ if .Merge }}
git config user.email "ci@flynn.io"
git config user.name "CI"
git merge origin/master
{{ end }}

make < /dev/null

script/kill-flynn

if [[ -f test/scripts/debug-info.sh ]]; then
  sudo cp test/scripts/debug-info.sh /usr/local/bin/debug-info.sh
fi

sudo cp build/bin/flynn-{host,init} /usr/local/bin
sudo cp build/manifests/bootstrap-manifest.json /etc/flynn-bootstrap.json
`[1:]))

type buildData struct {
	Commit string
	Merge  bool
}

func buildFlynn(inst *Instance, commit string, merge bool, out io.Writer) error {
	var b bytes.Buffer
	flynnBuildScript.Execute(&b, buildData{commit, merge})
	return inst.RunWithTimeout("bash", &Streams{Stdin: &b, Stdout: out, Stderr: out}, 30*time.Minute)
}

var flynnUnitTestScript = `
#!/bin/bash
set -e -x

export GOPATH=~/go
flynn=$GOPATH/src/github.com/flynn/flynn
cd $flynn

if [[ -f test/scripts/test-unit.sh ]]; then
  timeout --signal=QUIT --kill-after=10 5m test/scripts/test-unit.sh
fi
`[1:]

func runUnitTests(inst *Instance, out io.Writer) error {
	return inst.Run("bash", &Streams{Stdin: bytes.NewBufferString(flynnUnitTestScript), Stdout: out, Stderr: out})
}

type hostScriptData struct {
	ID    string
	IP    string
	Peers string
}

var flynnHostScript = template.Must(template.New("flynn-host").Parse(`
if [[ -f /usr/local/bin/debug-info.sh ]]; then
  /usr/local/bin/debug-info.sh &>/tmp/debug-info.log &
fi

# mount the backups dir
sudo mkdir -p /mnt/backups
sudo mount -t 9p -o trans=virtio backupsfs /mnt/backups

sudo start-stop-daemon \
  --start \
  --background \
  --no-close \
  --make-pidfile \
  --pidfile /var/run/flynn-host.pid \
  --exec /usr/bin/env \
  -- \
  flynn-host \
  daemon \
  --id {{ .ID }} \
  --external-ip {{ .IP }} \
  --listen-ip {{ .IP }} \
  --force \
  --peer-ips {{ .Peers }} \
  --max-job-concurrency 8 \
  --init-log-level debug \
  --tags "host_id={{ .ID }}" \
  &>/tmp/flynn-host.log
`[1:]))

type bootstrapMsg struct {
	Id    string          `json:"id"`
	State string          `json:"state"`
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error"`
}

type controllerCert struct {
	Pin string `json:"pin"`
}

func (c *Cluster) bootstrapLayer1(instances []*Instance) error {
	inst := instances[0]
	c.ClusterDomain = fmt.Sprintf("flynn-%s.local", random.String(16))
	c.ControllerKey = random.String(16)
	rd, wr := io.Pipe()

	ips := make([]string, len(instances))
	for i, inst := range instances {
		ips[i] = inst.IP
	}

	var cmdErr error
	go func() {
		command := fmt.Sprintf(
			"CLUSTER_DOMAIN=%s CONTROLLER_KEY=%s DISCOVERD=%s:1111 flynn-host bootstrap --json --min-hosts=%d --peer-ips=%s /etc/flynn-bootstrap.json",
			c.ClusterDomain, c.ControllerKey, inst.IP, len(instances), strings.Join(ips, ","),
		)
		cmdErr = inst.Run(command, &Streams{Stdout: wr, Stderr: c.out})
		wr.Close()
	}()

	// grab the controller tls pin from the bootstrap output
	var cert controllerCert
	dec := json.NewDecoder(rd)
	for {
		var msg bootstrapMsg
		if err := dec.Decode(&msg); err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("failed to parse bootstrap JSON output: %s", err)
		}
		c.log("bootstrap ===>", msg.Id, msg.State)
		if msg.State == "error" {
			c.log(msg.Error)
		}
		if msg.Id == "controller-cert" && msg.State == "done" {
			json.Unmarshal(msg.Data, &cert)
		}
	}
	if cmdErr != nil {
		return cmdErr
	}
	if cert.Pin == "" {
		return errors.New("could not determine controller cert from bootstrap output")
	}
	c.ControllerPin = cert.Pin

	// grab the router IP from discoverd
	disc := discoverd.NewClientWithURL(fmt.Sprintf("http://%s:1111", inst.IP))
	leader, err := disc.Service("router-api").Leader()
	if err != nil {
		return fmt.Errorf("could not detect router ip: %s", err)
	}
	c.RouterIP = leader.Host()
	return nil
}

func lookupUser(name string) (int, int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, 0, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return uid, gid, nil
}

func (c *Cluster) DumpLogs(buildLog *buildlog.Log) {
	run := func(log string, inst *Instance, cmds ...string) error {
		out, err := buildLog.NewFileWithTimeout(log, 60*time.Second)
		if err != nil {
			return err
		}
		for _, cmd := range cmds {
			fmt.Fprintln(out, "HostID:", inst.ID, "-", cmd)
			fmt.Fprintln(out)
			script := fmt.Sprintf(`/usr/bin/env DISCOVERD="http://%s:1111" %s`, inst.IP, cmd)
			err := inst.Run("bash", &Streams{Stdin: strings.NewReader(script), Stdout: out, Stderr: out})
			fmt.Fprintln(out)
			if err != nil {
				return err
			}
		}
		return nil
	}
	for _, inst := range c.Instances {
		run(
			fmt.Sprintf("host-logs-%s.log", inst.ID),
			inst,
			"ps faux",
			"cat /var/log/flynn/flynn-host.log",
			"cat /tmp/debug-info.log",
		)
	}

	printLogs := func(typ string, instances []*Instance) {
		fallback := func(instances []*Instance) {
			for _, inst := range instances {
				run(fmt.Sprintf("%s-fallback-%s.log", typ, inst.ID), inst, "sudo bash -c 'tail -n +1 /var/log/flynn/*.log'")
			}
		}

		run(fmt.Sprintf("%s-jobs.log", typ), instances[0], "flynn-host ps -a")

		var out bytes.Buffer
		cmd := `flynn-host ps -aqf '{{ metadata "flynn-controller.app_name" }}:{{ metadata "flynn-controller.type" }}:{{ .Job.ID }}'`
		if err := instances[0].Run(cmd, &Streams{Stdout: &out, Stderr: &out}); err != nil {
			fallback(instances)
			return
		}

		// only fallback if all `flynn-host log` commands fail
		shouldFallback := true
		jobs := strings.Split(strings.TrimSpace(out.String()), "\n")
		for _, job := range jobs {
			fields := strings.Split(job, ":")
			jobID := fields[2]
			cmds := []string{
				fmt.Sprintf("timeout 10s flynn-host inspect --redact-env BACKEND_S3 %s", jobID),
				fmt.Sprintf("timeout 10s flynn-host log --init %s", jobID),
			}
			if err := run(fmt.Sprintf("%s-%s.log", typ, job), instances[0], cmds...); err != nil {
				continue
			}
			shouldFallback = false
		}
		if shouldFallback {
			fallback(instances)
		}

		// run the fallback on any stopped instances as their logs will
		// not appear in `flynn-host ps`
		stoppedInstances := make([]*Instance, 0, len(instances))
		for _, inst := range instances {
			if err := inst.Run("sudo kill -0 $(cat /var/run/flynn-host.pid)", nil); err != nil {
				stoppedInstances = append(stoppedInstances, inst)
			}
		}
		if len(stoppedInstances) > 0 {
			fallback(stoppedInstances)
		}
	}
	if len(c.defaultInstances) > 0 {
		printLogs("default", c.defaultInstances)
	}
	if len(c.releaseInstances) > 0 {
		printLogs("release", c.releaseInstances)
	}
}
