package cluster

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/random"
)

func NewVMManager(bridge *Bridge) *VMManager {
	return &VMManager{taps: &TapManager{bridge}}
}

type VMManager struct {
	taps *TapManager
}

type VMConfig struct {
	Kernel string
	User   int
	Group  int
	Memory string
	Cores  int
	Drives map[string]*VMDrive
	Args   []string
	Out    io.Writer `json:"-"`

	netFS string
}

type VMDrive struct {
	FS   string
	COW  bool
	Temp bool
}

func (v *VMManager) NewInstance(c *VMConfig) (*Instance, error) {
	var err error
	inst := &Instance{ID: random.String(8), VMConfig: c}
	if c.Kernel == "" {
		c.Kernel = "vmlinuz"
	}
	if c.Out == nil {
		c.Out, err = os.Create("flynn-" + inst.ID + ".log")
		if err != nil {
			return nil, err
		}
	}
	inst.tap, err = v.taps.NewTap(c.User, c.Group)
	if err != nil {
		return nil, err
	}
	inst.IP = inst.tap.IP.String()
	return inst, nil
}

type Instance struct {
	ID string `json:"id"`
	IP string `json:"ip"`

	*VMConfig
	tap *Tap
	cmd *exec.Cmd

	tempFiles []string

	sshMtx sync.RWMutex
	ssh    *ssh.Client

	initial bool
}

func (i *Instance) writeInterfaceConfig() error {
	dir, err := ioutil.TempDir("", "netfs-")
	if err != nil {
		return err
	}
	i.tempFiles = append(i.tempFiles, dir)
	i.netFS = dir

	if err := os.Chmod(dir, 0755); err != nil {
		os.RemoveAll(dir)
		return err
	}

	f, err := os.Create(filepath.Join(dir, "eth0"))
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	defer f.Close()

	return i.tap.WriteInterfaceConfig(f)
}

func (i *Instance) cleanup() {
	for _, f := range i.tempFiles {
		fmt.Printf("removing temp file %s\n", f)
		if err := os.RemoveAll(f); err != nil {
			fmt.Printf("could not remove temp file %s: %s\n", f, err)
		}
	}
	if err := i.tap.Close(); err != nil {
		fmt.Printf("could not close tap device %s: %s\n", i.tap.Name, err)
	}
	i.tempFiles = nil

	i.sshMtx.Lock()
	defer i.sshMtx.Unlock()
	if i.ssh != nil {
		i.ssh.Close()
	}
}

func (i *Instance) Start() error {
	i.writeInterfaceConfig()

	macRand := random.Bytes(3)
	macaddr := fmt.Sprintf("52:54:00:%02x:%02x:%02x", macRand[0], macRand[1], macRand[2])

	i.Args = append(i.Args,
		"-enable-kvm",
		"-kernel", i.Kernel,
		"-append", `"root=/dev/sda"`,
		"-netdev", "tap,id=vmnic,ifname="+i.tap.Name+",script=no,downscript=no",
		"-device", "virtio-net,netdev=vmnic,mac="+macaddr,
		"-virtfs", "fsdriver=local,path="+i.netFS+",security_model=passthrough,readonly,mount_tag=netfs",
		"-nographic",
	)
	if i.Memory != "" {
		i.Args = append(i.Args, "-m", i.Memory)
	}
	if i.Cores > 0 {
		i.Args = append(i.Args, "-smp", strconv.Itoa(i.Cores))
	}
	var err error
	for n, d := range i.Drives {
		if d.COW {
			fs, err := i.createCOW(d.FS, d.Temp)
			if err != nil {
				i.cleanup()
				return err
			}
			d.FS = fs
		}
		i.Args = append(i.Args, fmt.Sprintf("-%s", n), d.FS)
	}

	i.cmd = exec.Command("sudo", append([]string{"-u", fmt.Sprintf("#%d", i.User), "-g", fmt.Sprintf("#%d", i.Group), "-H", "/usr/bin/qemu-system-x86_64"}, i.Args...)...)
	i.cmd.Stdout = i.Out
	i.cmd.Stderr = i.Out
	if err = i.cmd.Start(); err != nil {
		i.cleanup()
	}
	return err
}

func (i *Instance) createCOW(image string, temp bool) (string, error) {
	name := strings.TrimSuffix(filepath.Base(image), filepath.Ext(image))
	dir, err := ioutil.TempDir("", name+"-")
	if err != nil {
		return "", err
	}
	if temp {
		i.tempFiles = append(i.tempFiles, dir)
	}
	if err := os.Chown(dir, i.User, i.Group); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "rootfs.img")
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", "-b", image, path)
	if err = cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create COW filesystem: %s", err.Error())
	}
	if err := os.Chown(path, i.User, i.Group); err != nil {
		return "", err
	}
	return path, nil
}

func (i *Instance) Wait(timeout time.Duration) error {
	done := make(chan error)
	go func() {
		done <- i.cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return errors.New("timeout")
	}
}

func (i *Instance) Reboot() error {
	if err := i.Run("sudo reboot && sleep 60", nil); err != nil {
		return i.Kill()
	}
	i.sshMtx.Lock()
	i.ssh.Close()
	i.ssh = nil
	i.sshMtx.Unlock()
	return i.Run("uptime", nil)
}

func (i *Instance) Shutdown() error {
	if err := i.Run("sudo poweroff", nil); err != nil {
		return i.Kill()
	}
	if err := i.Wait(5 * time.Second); err != nil {
		return i.Kill()
	}
	i.cleanup()
	return nil
}

func (i *Instance) Kill() error {
	defer i.cleanup()
	if err := i.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	if err := i.Wait(5 * time.Second); err != nil {
		return i.cmd.Process.Kill()
	}
	return nil
}

func (i *Instance) dialSSH(stderr io.Writer) error {
	if i.ssh != nil {
		return nil
	}

	i.sshMtx.RUnlock()
	i.sshMtx.Lock()
	defer i.sshMtx.RLock()
	defer i.sshMtx.Unlock()

	if i.ssh != nil {
		return nil
	}

	var sc *ssh.Client
	err := sshAttempts.Run(func() (err error) {
		if stderr != nil {
			fmt.Fprintf(stderr, "Attempting to ssh to %s:22...\n", i.IP)
		}
		sc, err = ssh.Dial("tcp", i.IP+":22", &ssh.ClientConfig{
			User: "ubuntu",
			Auth: []ssh.AuthMethod{ssh.Password("ubuntu")},
		})
		return
	})
	if sc != nil {
		i.ssh = sc
	}
	return err
}

var sshAttempts = attempt.Strategy{
	Min:   5,
	Total: 30 * time.Second,
	Delay: time.Second,
}

func (i *Instance) Run(command string, s *Streams) error {
	if s == nil {
		s = &Streams{}
	}

	i.sshMtx.RLock()
	defer i.sshMtx.RUnlock()
	if err := i.dialSSH(s.Stderr); err != nil {
		return err
	}

	sess, err := i.ssh.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	sess.Stdin = s.Stdin
	sess.Stdout = s.Stdout
	sess.Stderr = s.Stderr
	if err := sess.Run(command); err != nil {
		return fmt.Errorf("failed to run command on %s: %s", i.IP, err)
	}
	return nil
}

func (i *Instance) Drive(name string) *VMDrive {
	return i.Drives[name]
}
