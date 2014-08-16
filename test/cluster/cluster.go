package cluster

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"text/template"

	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/random"
)

type BootConfig struct {
	User     string
	Kernel   string
	Network  string
	NatIface string
}

type Cluster struct {
	ControllerDomain string
	ControllerPin    string
	ControllerKey    string
	RouterIP         string

	bc        BootConfig
	vm        *VMManager
	instances []Instance
	out       io.Writer
	bridge    *Bridge
}

type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func New(bc BootConfig, out io.Writer) *Cluster {
	return &Cluster{
		bc:  bc,
		out: out,
	}
}

func BuildFlynn(bc BootConfig, rootFS, commit string, out io.Writer) (string, error) {
	c := New(bc, out)
	defer c.Shutdown()
	return c.BuildFlynn(rootFS, commit)
}

func (c *Cluster) log(a ...interface{}) (int, error) {
	return fmt.Fprintln(c.out, a...)
}

func (c *Cluster) logf(f string, a ...interface{}) (int, error) {
	return fmt.Fprintf(c.out, f, a...)
}

func (c *Cluster) BuildFlynn(rootFS, commit string) (string, error) {
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
		Memory: "2048",
		Cores:  8,
		Drives: map[string]*VMDrive{
			"hda": {FS: rootFS, COW: true, Temp: false},
		},
	})
	if err != nil {
		return "", err
	}
	c.log("Booting build instance...")
	if err := build.Start(); err != nil {
		return "", fmt.Errorf("error starting build instance: %s", err)
	}

	c.log("Waiting for instance to boot...")
	if err := buildFlynn(build, commit, c.out); err != nil {
		build.Kill()
		return "", fmt.Errorf("error running build script: %s", err)
	}

	if err := build.Shutdown(); err != nil {
		return "", fmt.Errorf("error while stopping build instance: %s", err)
	}
	return build.Drive("hda").FS, nil
}

func (c *Cluster) Boot(backend, rootFS string, count int) error {
	if err := c.setup(); err != nil {
		return err
	}
	uid, gid, err := lookupUser(c.bc.User)
	if err != nil {
		return err
	}

	c.log("Booting", count, "instances")
	for i := 0; i < count; i++ {
		inst, err := c.vm.NewInstance(&VMConfig{
			Kernel: c.bc.Kernel,
			User:   uid,
			Group:  gid,
			Memory: "512",
			Drives: map[string]*VMDrive{
				"hda": {FS: rootFS, COW: true, Temp: true},
			},
		})
		if err != nil {
			c.Shutdown()
			return fmt.Errorf("error creating instance %d: %s", i, err)
		}
		if err = inst.Start(); err != nil {
			c.Shutdown()
			return fmt.Errorf("error starting instance %d: %s", i, err)
		}
		c.instances = append(c.instances, inst)
	}

	c.log("Bootstrapping layer 0...")
	if err := c.bootstrapGrid(backend); err != nil {
		c.Shutdown()
		return err
	}
	c.log("Bootstrapping layer 1...")
	if err := c.bootstrapFlynn(); err != nil {
		c.Shutdown()
		return err
	}
	return nil
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
	if len(c.instances) == 0 {
		return errors.New("no booted servers in cluster")
	}
	return c.instances[0].Run(command, s)
}

func (c *Cluster) CLIConfig() (*config.Config, error) {
	conf := &config.Config{}
	s := &config.Server{
		Name:    "default",
		URL:     "https://" + c.ControllerDomain,
		Key:     c.ControllerKey,
		GitHost: c.ControllerDomain + ":2222",
		TLSPin:  c.ControllerPin,
	}
	if err := conf.Add(s); err != nil {
		return nil, err
	}
	return conf, nil
}

func (c *Cluster) Shutdown() {
	for i, inst := range c.instances {
		c.log("killing instance", i)
		if err := inst.Kill(); err != nil {
			c.logf("error killing instance %d: %s\n", i, err)
		}
	}
	if c.bridge != nil {
		c.logf("deleting network bridge %s\n", c.bridge.name)
		if err := deleteBridge(c.bridge); err != nil {
			c.logf("error deleting network bridge %s: %s\n", c.bridge.name, err)
		}
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
git checkout --quiet {{ . }}
tup

sudo cp {host/bin/flynn-*,pinkerton/pinkerton,bootstrap/bin/flynn-bootstrap} /usr/bin
sudo cp host/bin/manifest.json /etc/flynn-host.json
sudo cp bootstrap/bin/manifest.json /etc/flynn-bootstrap.json
`[1:]))

func buildFlynn(inst Instance, commit string, out io.Writer) error {
	var b bytes.Buffer
	flynnBuildScript.Execute(&b, commit)
	return inst.Run("bash", &Streams{Stdin: &b, Stdout: out, Stderr: out})
}

func (c *Cluster) bootstrapGrid(backend string) error {
	for i, inst := range c.instances {
		var etcdPeers string
		if i > 0 {
			etcdPeers = fmt.Sprintf("%s:7001", c.instances[0].IP())
		}

		var command string
		switch backend {
		case "libvirt-lxc":
			command = fmt.Sprintf("sudo virsh net-start default; sudo start-stop-daemon --start --background --no-close --exec /usr/bin/env -- ETCD_PEERS=%s flynn-host -manifest /etc/flynn-host.json -external %s -force -backend %s &>/tmp/flynn-host.log", etcdPeers, inst.IP(), backend)
		case "docker":
			command = fmt.Sprintf("docker run -d -v=/var/run/docker.sock:/var/run/docker.sock -p=1113:1113 -e=ETCD_PEERS=%s flynn/host -external %s -force -backend %s", etcdPeers, inst.IP(), backend)
		}

		if err := inst.Run(command, &Streams{Stdout: c.out, Stderr: os.Stderr}); err != nil {
			return err
		}
	}
	return nil
}

type bootstrapMsg struct {
	Id    string          `json:"id"`
	State string          `json:"state"`
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error"`
}

type controllerCert struct {
	Pin string `json:"pin"`
}

func (c *Cluster) bootstrapFlynn() error {
	inst := c.instances[0]
	c.ControllerDomain = fmt.Sprintf("flynn-%s.local", random.String(16))
	c.ControllerKey = random.String(16)
	rd, wr := io.Pipe()
	var cmdErr error
	go func() {
		command := fmt.Sprintf(
			"DISCOVERD=%s:1111 CONTROLLER_DOMAIN=%s CONTROLLER_KEY=%s flynn-bootstrap -json -min-hosts=%d /etc/flynn-bootstrap.json",
			inst.IP(), c.ControllerDomain, c.ControllerKey, len(c.instances),
		)
		cmdErr = inst.Run(command, &Streams{Stdout: wr, Stderr: os.Stderr})
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
	disc, err := discoverd.NewClientWithAddr(inst.IP() + ":1111")
	if err != nil {
		return fmt.Errorf("could not connect to discoverd at %s:1111: %s", inst.IP(), err)
	}
	defer disc.Close()
	set, err := disc.NewServiceSet("router-api")
	if err != nil {
		return fmt.Errorf("could not detect router ip: %s", err)
	}
	defer set.Close()
	leader := set.Leader()
	if leader == nil {
		return errors.New("could not detect router ip: no router-api leader")
	}
	if err = setLocalDNS(c.ControllerDomain, leader.Host); err != nil {
		return fmt.Errorf("could not set router DNS entry: %s", err)
	}
	c.RouterIP = leader.Host
	return nil
}

func setLocalDNS(domain, ip string) error {
	command := fmt.Sprintf(
		`grep -q "^%[1]s" /etc/hosts && sed "s/^%[1]s.*/%[1]s %s/" -i /etc/hosts || echo %[1]s %s >> /etc/hosts`,
		ip, domain,
	)
	cmd := exec.Command("bash", "-c", command)
	return cmd.Run()
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
