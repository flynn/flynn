package cluster

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"text/template"
	"time"

	"github.com/flynn/flynn-test/util"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-flynn/attempt"
)

type BootConfig struct {
	User     string
	RootFS   string
	Kernel   string
	Network  string
	NatIface string
}

type Cluster struct {
	ControllerDomain string
	ControllerPin    string
	ControllerKey    string

	bc        BootConfig
	vm        *VMManager
	instances []Instance
	out       io.Writer
	bridge    *Bridge
}

func New(bc BootConfig, out io.Writer) *Cluster {
	return &Cluster{
		bc:  bc,
		out: out,
	}
}

func BuildFlynn(bc BootConfig, dockerFS string, repos map[string]string, out io.Writer) (string, error) {
	c := New(bc, out)
	defer c.Shutdown()
	return c.BuildFlynn(dockerFS, repos)
}

func (c *Cluster) log(a ...interface{}) (int, error) {
	return fmt.Fprintln(c.out, a...)
}

func (c *Cluster) logf(f string, a ...interface{}) (int, error) {
	return fmt.Fprintf(c.out, f, a...)
}

func (c *Cluster) BuildFlynn(dockerFS string, repos map[string]string) (string, error) {
	c.log("Building Flynn...")

	if err := c.setup(); err != nil {
		return "", err
	}

	uid, gid, err := lookupUser(c.bc.User)
	if err != nil {
		return "", err
	}

	dockerDrive := VMDrive{FS: dockerFS, COW: true, Temp: false}
	if dockerDrive.FS == "" {
		// create 16GB sparse fs image to store docker data on
		dockerFS, err := createBtrfs(17179869184, "dockerfs", uid, gid)
		if err != nil {
			os.RemoveAll(dockerFS)
			return "", err
		}
		dockerDrive.FS = dockerFS
		dockerDrive.COW = false
	}

	build, err := c.vm.NewInstance(&VMConfig{
		Kernel: c.bc.Kernel,
		User:   uid,
		Group:  gid,
		Memory: "512",
		Drives: map[string]*VMDrive{
			"hda": &VMDrive{FS: c.bc.RootFS, COW: true, Temp: true},
			"hdb": &dockerDrive,
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
	if err := buildFlynn(build, repos, c.out); err != nil {
		build.Kill()
		return "", fmt.Errorf("error running build script: %s", err)
	}

	if err := build.Kill(); err != nil {
		return "", fmt.Errorf("error while stopping build instance: %s", err)
	}
	return build.Drive("hdb").FS, nil
}

func (c *Cluster) Boot(dockerfs string, count int) error {
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
				"hda": &VMDrive{FS: c.bc.RootFS, COW: true, Temp: true},
				"hdb": &VMDrive{FS: dockerfs, COW: true, Temp: true},
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
	if err := c.bootstrapGrid(); err != nil {
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
		name := "flynnbr." + util.RandomString(5)
		c.logf("creating network bridge %s\n", name)
		c.bridge, err = createBridge(name, c.bc.Network, c.bc.NatIface)
		if err != nil {
			return fmt.Errorf("could not create network bridge: %s", err)
		}
	}
	c.vm = NewVMManager(c.bridge)
	return nil
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

var attempts = attempt.Strategy{
	Min:   5,
	Total: 5 * time.Minute,
	Delay: time.Second,
}

var flynnBuildScript = template.Must(template.New("flynn-build").Parse(`
#!/bin/bash
set -e -x

export GOPATH=/var/lib/docker/flynn/go
flynn=$GOPATH/src/github.com/flynn
sudo mkdir -p $flynn
sudo chown -R ubuntu:ubuntu $GOPATH

build() {
  repo=$1
  ref=$2
  dir=$flynn/$repo
  test -d $dir || git clone https://github.com/flynn/$repo $dir
  pushd $dir > /dev/null
  git fetch
  git checkout $ref
  rm -rf /tmp/godep # work around godep bugs
  test -f Makefile && make clean && make
  popd > /dev/null
}

{{ range $repo, $ref := . }}
build "{{ $repo }}" "{{ $ref }}"
{{ end }}

sudo stop docker
sudo umount /var/lib/docker
`[1:]))

func buildFlynn(inst Instance, repos map[string]string, out io.Writer) error {
	var b bytes.Buffer
	flynnBuildScript.Execute(&b, repos)
	return inst.Run(b.String(), attempts, out, out)
}

func (c *Cluster) bootstrapGrid() error {
	for i, inst := range c.instances {
		command := "docker run -d -v=/var/run/docker.sock:/var/run/docker.sock -p=1113:1113"
		if i > 0 {
			command = fmt.Sprintf("%s -e=ETCD_PEERS=%s:7001", command, c.instances[0].IP())
		}
		command = fmt.Sprintf("%s flynn/host -external %s -force", command, inst.IP())
		if err := inst.Run(command, attempts, c.out, os.Stderr); err != nil {
			return err
		}
	}
	return nil
}

type bootstrapMsg struct {
	Id    string          `json:"id"`
	State string          `json:"state"`
	Data  json.RawMessage `json:"data"`
}

type controllerCert struct {
	Pin string `json:"pin"`
}

func (c *Cluster) bootstrapFlynn() error {
	inst := c.instances[0]
	c.ControllerDomain = fmt.Sprintf("flynn-%s.local", util.RandomString(16))
	c.ControllerKey = util.RandomString(16)
	rd, wr := io.Pipe()
	var cmdErr error
	go func() {
		command := fmt.Sprintf(
			"docker run -e=DISCOVERD=%s:1111 -e CONTROLLER_DOMAIN=%s -e CONTROLLER_KEY=%s flynn/bootstrap -json -min-hosts=%d /etc/manifest.json",
			inst.IP(), c.ControllerDomain, c.ControllerKey, len(c.instances),
		)
		cmdErr = inst.Run(command, attempts, wr, os.Stderr)
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
			c.log(msg)
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

	// grab the strowger IP from discoverd
	discoverd.Connect(inst.IP() + ":1111")
	set, err := discoverd.NewServiceSet("strowger-api")
	if err != nil {
		return fmt.Errorf("could not detect strowger ip: %s", err)
	}
	defer set.Close()
	leader := set.Leader()
	if leader == nil {
		return errors.New("could not detect strowger ip: no strowger-api leader")
	}
	if err = setLocalDNS(c.ControllerDomain, leader.Host); err != nil {
		return fmt.Errorf("could not set strowger DNS entry:", err)
	}
	return nil
}

func createBtrfs(size int64, label string, uid, gid int) (string, error) {
	f, err := ioutil.TempFile("", label+"-")
	if err != nil {
		return "", err
	}
	if _, err := f.Seek(size, 0); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if _, err := f.Write([]byte{0}); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Chown(uid, gid)
	f.Close()

	res, err := exec.Command("mkfs.btrfs", "--label", label, f.Name()).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mkfs.btrfs error %s - %q", err, res)
	}
	return f.Name(), nil
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
