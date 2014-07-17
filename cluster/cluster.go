package cluster

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
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
	NatIface string
}

type Cluster struct {
	ControllerDomain string
	ControllerPin    string
	ControllerKey    string

	bc        BootConfig
	vm        *VMManager
	instances []Instance
}

func New(bc BootConfig) *Cluster {
	return &Cluster{
		bc: bc,
		vm: NewVMManager(),
	}
}

func BuildFlynn(bc BootConfig, dockerFS string, repos map[string]string) (string, error) {
	return New(bc).BuildFlynn(dockerFS, repos)
}

func (c *Cluster) BuildFlynn(dockerFS string, repos map[string]string) (string, error) {
	fmt.Println("Building Flynn...")

	if err := c.setup(); err != nil {
		return "", err
	}

	uid, gid, err := lookupUser(c.bc.User)
	if err != nil {
		return "", err
	}

	dockerDrive := VMDrive{FS: dockerFS, TempCOW: true}
	if dockerDrive.FS == "" {
		// create 16GB sparse fs image to store docker data on
		dockerFS, err := createBtrfs(17179869184, "dockerfs", uid, gid)
		if err != nil {
			os.RemoveAll(dockerFS)
			return "", err
		}
		dockerDrive.FS = dockerFS
		dockerDrive.TempCOW = false
	}

	build, err := c.vm.NewInstance(&VMConfig{
		Kernel: c.bc.Kernel,
		User:   uid,
		Group:  gid,
		Memory: "512",
		Drives: map[string]VMDrive{
			"hda": VMDrive{FS: c.bc.RootFS, TempCOW: true},
			"hdb": dockerDrive,
		},
	})
	if err != nil {
		return "", err
	}
	fmt.Println("Booting build instance...")
	if err := build.Start(); err != nil {
		return "", fmt.Errorf("error starting build instance: %s", err)
	}

	fmt.Println("Waiting for instance to boot...")
	if err := buildFlynn(build, repos); err != nil {
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

	fmt.Println("Booting", count, "instances")
	for i := 0; i < count; i++ {
		inst, err := c.vm.NewInstance(&VMConfig{
			Kernel: c.bc.Kernel,
			User:   uid,
			Group:  gid,
			Memory: "512",
			Drives: map[string]VMDrive{
				"hda": VMDrive{FS: c.bc.RootFS, TempCOW: true},
				"hdb": VMDrive{FS: dockerfs, TempCOW: true},
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

	fmt.Println("Bootstrapping layer 0...")
	if err := c.bootstrapGrid(); err != nil {
		c.Shutdown()
		return err
	}
	fmt.Println("Bootstrapping layer 1...")
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
	fmt.Println("Initializing networking...")
	if err := initNetworking(c.bc.NatIface); err != nil {
		return fmt.Errorf("cluster: net init error: %s", err)
	}
	return nil
}

func (c *Cluster) Shutdown() {
	for i, inst := range c.instances {
		log.Println("killing instance", i)
		if err := inst.Kill(); err != nil {
			log.Printf("error killing instance %d: %s\n", i, err)
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

func buildFlynn(inst Instance, repos map[string]string) error {
	var b bytes.Buffer
	flynnBuildScript.Execute(&b, repos)
	return inst.Run(b.String(), attempts, os.Stdout)
}

func (c *Cluster) bootstrapGrid() error {
	for i, inst := range c.instances {
		command := "docker run -d -v=/var/run/docker.sock:/var/run/docker.sock -p=1113:1113"
		if i > 0 {
			command = fmt.Sprintf("%s -e=ETCD_PEERS=%s:7001", command, c.instances[0].IP())
		}
		command = fmt.Sprintf("%s flynn/host -external %s -force", command, inst.IP())
		if err := inst.Run(command, attempts, os.Stdout); err != nil {
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
	c.ControllerDomain = fmt.Sprintf("flynn-%s.local", util.RandomString())
	c.ControllerKey = util.RandomString()
	rd, wr := io.Pipe()
	var cmdErr error
	go func() {
		command := fmt.Sprintf(
			"docker run -e=DISCOVERD=%s:1111 -e CONTROLLER_DOMAIN=%s -e CONTROLLER_KEY=%s flynn/bootstrap -json -min-hosts=%d /etc/manifest.json",
			inst.IP(), c.ControllerDomain, c.ControllerKey, len(c.instances),
		)
		cmdErr = inst.Run(command, attempts, wr)
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
		fmt.Println("bootstrap ===>", msg.Id, msg.State)
		if msg.State == "error" {
			fmt.Println(msg)
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
