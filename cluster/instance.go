package cluster

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"code.google.com/p/go.crypto/ssh"
	"github.com/flynn/go-flynn/attempt"
)

func NewVMManager() *VMManager {
	return &VMManager{taps: &TapManager{}}
}

type VMManager struct {
	taps   *TapManager
	nextID uint64
}

type VMConfig struct {
	Kernel string
	User   int
	Group  int
	Memory string
	Drives map[string]VMDrive
	Args   []string
	Out    io.Writer

	netFS string
}

type VMDrive struct {
	FS  string
	COW string

	TempCOW bool
}

func (v *VMManager) NewInstance(c *VMConfig) (Instance, error) {
	id := atomic.AddUint64(&v.nextID, 1) - 1
	inst := &vm{
		ID:       fmt.Sprintf("flynn%d", id),
		VMConfig: c,
	}
	if c.Kernel == "" {
		c.Kernel = "vmlinuz"
	}
	if c.Out == nil {
		var err error
		c.Out, err = os.Create(inst.ID + ".log")
		if err != nil {
			return nil, err
		}
	}
	var err error
	inst.tap, err = v.taps.NewTap(c.User, c.Group)
	return inst, err
}

type Instance interface {
	DialSSH() (*ssh.Client, error)
	Start() error
	Wait() error
	Kill() error
	IP() string
	Run(string, attempt.Strategy, io.Writer) error
	Drive(string) VMDrive
}

type vm struct {
	ID string
	*VMConfig
	tap *Tap
	cmd *exec.Cmd

	tempFiles []string
}

func (v *vm) writeInterfaceConfig() error {
	dir, err := ioutil.TempDir("", "netfs-")
	if err != nil {
		return err
	}
	v.tempFiles = append(v.tempFiles, dir)
	v.netFS = dir

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

	return v.tap.WriteInterfaceConfig(f)
}

func (v *vm) cleanup() {
	for _, f := range v.tempFiles {
		if err := os.RemoveAll(f); err != nil {
			fmt.Printf("could not remove temp file %s: %s\n", f, err)
		}
	}
	if err := v.tap.Close(); err != nil {
		fmt.Printf("could not close tap device %s: %s\n", v.tap.Name, err)
	}
	v.tempFiles = nil
}

func (v *vm) Start() error {
	v.writeInterfaceConfig()

	macRand := make([]byte, 3)
	io.ReadFull(rand.Reader, macRand)
	macaddr := fmt.Sprintf("52:54:00:%02x:%02x:%02x", macRand[0], macRand[1], macRand[2])

	v.Args = append(v.Args,
		"-enable-kvm",
		"-kernel", v.Kernel,
		"-append", `"root=/dev/sda"`,
		"-net", "nic,macaddr="+macaddr,
		"-net", "tap,ifname="+v.tap.Name+",script=no,downscript=no",
		"-virtfs", "fsdriver=local,path="+v.netFS+",security_model=passthrough,readonly,mount_tag=netfs",
		"-nographic",
	)
	if v.Memory != "" {
		v.Args = append(v.Args, "-m", v.Memory)
	}
	var err error
	for i, d := range v.Drives {
		fs := d.FS
		if d.TempCOW {
			fs, err = v.tempCOW(d.FS)
			if err != nil {
				v.cleanup()
				return err
			}
		}
		v.Args = append(v.Args, fmt.Sprintf("-%s", i), fs)
	}

	v.cmd = exec.Command("sudo", append([]string{"-u", fmt.Sprintf("#%d", v.User), "-g", fmt.Sprintf("#%d", v.Group), "-H", "/usr/bin/qemu-system-x86_64"}, v.Args...)...)
	v.cmd.Stdout = v.Out
	v.cmd.Stderr = v.Out
	if err = v.cmd.Start(); err != nil {
		v.cleanup()
	}
	return err
}

func (v *vm) tempCOW(image string) (string, error) {
	name := strings.TrimSuffix(filepath.Base(image), filepath.Ext(image))
	dir, err := ioutil.TempDir("", name+"-")
	if err != nil {
		return "", err
	}
	v.tempFiles = append(v.tempFiles, dir)
	if err := os.Chown(dir, v.User, v.Group); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "fs.img")
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", "-b", image, path)
	if err = cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create COW filesystem: %s", err.Error())
	}
	if err := os.Chown(path, v.User, v.Group); err != nil {
		return "", err
	}
	return path, nil
}

func (v *vm) Wait() error {
	defer v.cleanup()
	return v.cmd.Wait()
}

func (v *vm) Kill() error {
	defer v.cleanup()
	if err := v.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	done := make(chan error)
	go func() {
		done <- v.cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		return v.cmd.Process.Kill()
	}
}

func (v *vm) DialSSH() (*ssh.Client, error) {
	return ssh.Dial("tcp", v.IP()+":22", &ssh.ClientConfig{
		User: "ubuntu",
		Auth: []ssh.AuthMethod{ssh.Password("ubuntu")},
	})
}

func (v *vm) IP() string {
	return v.tap.RemoteIP.String()
}

func (v *vm) Run(command string, attempts attempt.Strategy, out io.Writer) error {
	var sc *ssh.Client
	err := attempts.Run(func() (err error) {
		fmt.Printf("Attempting to ssh to %s:22...\n", v.IP())
		sc, err = v.DialSSH()
		return
	})
	if err != nil {
		return err
	}
	defer sc.Close()
	sess, err := sc.NewSession()
	sess.Stdin = bytes.NewBufferString(command)
	sess.Stdout = out
	sess.Stderr = os.Stderr
	if err := sess.Run("bash"); err != nil {
		return fmt.Errorf("failed to run command on %s: %s", v.IP(), err)
	}
	return nil
}

func (v *vm) Drive(name string) VMDrive {
	return v.Drives[name]
}
