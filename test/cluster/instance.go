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
	"time"

	units "github.com/docker/go-units"
	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/resource"
	host "github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
	"golang.org/x/crypto/ssh"
)

func NewVMManager(client controller.Client) *VMManager {
	return &VMManager{client}
}

type VMManager struct {
	client controller.Client
}

type VMConfig struct {
	Kernel     string
	Memory     int
	Cores      int
	Disk       *VMDisk
	Args       []string
	Out        io.Writer `json:"-"`
	BackupsDir string

	netFS string
}

type VMDisk struct {
	FS   string
	COW  bool
	Temp bool
}

func (v *VMManager) NewInstance(c *VMConfig) (*Instance, error) {
	inst := &Instance{
		ID:       random.String(8),
		VMConfig: c,
		client:   v.client,
	}
	if c.Kernel == "" {
		c.Kernel = "vmlinuz"
	}
	if c.Out == nil {
		var err error
		c.Out, err = os.Create("flynn-" + inst.ID + ".log")
		if err != nil {
			return nil, err
		}
	}
	return inst, nil
}

type Instance struct {
	ID string `json:"id"`
	IP string `json:"ip"`

	*VMConfig
	client controller.Client
	job    *ct.Job

	tempFiles []string

	sshMtx sync.RWMutex
	ssh    *ssh.Client

	initial bool
}

func (i *Instance) cleanup() {
	for _, f := range i.tempFiles {
		fmt.Printf("removing temp file %s\n", f)
		if err := os.RemoveAll(f); err != nil {
			fmt.Printf("could not remove temp file %s: %s\n", f, err)
		}
	}
	i.tempFiles = nil

	i.sshMtx.Lock()
	defer i.sshMtx.Unlock()
	if i.ssh != nil {
		i.ssh.Close()
	}
}

func (i *Instance) Start() error {
	// create a copy-on-write disk if necessary
	if i.Disk.COW {
		fs, err := i.createCOW(i.Disk.FS, i.Disk.Temp)
		if err != nil {
			i.cleanup()
			return err
		}
		i.Disk.FS = fs
	}

	// stream job events so we can grab the IP once the job has started
	events := make(chan *ct.Job)
	stream, err := i.client.StreamJobEvents(os.Getenv("FLYNN_APP_ID"), events)
	if err != nil {
		i.cleanup()
		return err
	}
	defer stream.Close()

	// run the VM as a one-off job
	newJob := &ct.NewJob{
		ReleaseID: os.Getenv("FLYNN_RELEASE_ID"),
		Args:      []string{"/bin/run-vm.sh"},
		Env: map[string]string{
			"MEMORY": strconv.Itoa(i.Memory),
			"CPUS":   strconv.Itoa(i.Cores),
			"DISK":   i.Disk.FS,
			"KERNEL": i.Kernel,
		},
		MountsFrom: "runner",
		Resources:  resource.Defaults(),
		Profiles:   []host.JobProfile{host.JobProfileKVM},
	}
	newJob.Resources.SetLimit(resource.TypeMemory, int64(i.Memory*units.MiB))
	i.job, err = i.client.RunJobDetached(os.Getenv("FLYNN_APP_ID"), newJob)
	if err != nil {
		i.cleanup()
		return err
	}

	timeout := time.After(30 * time.Second)
	for {
		select {
		case job, ok := <-events:
			if !ok {
				i.cleanup()
				return stream.Err()
			}
			if job.ID != i.job.ID {
				continue
			}
			switch job.State {
			case ct.JobStateDown:
				i.cleanup()
				return errors.New("job stopped")
			case ct.JobStateUp:
				host, err := cluster.NewClient().Host(job.HostID)
				if err != nil {
					i.cleanup()
					return err
				}
				hostJob, err := host.GetJob(job.ID)
				if err != nil {
					i.cleanup()
					return err
				}
				i.IP = hostJob.InternalIP
				return nil
			}
		case <-timeout:
			i.cleanup()
			return errors.New("timed out waiting for job to start")
		}
	}
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
	path := filepath.Join(dir, "rootfs.img")
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", "-b", image, path)
	if err = cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create COW filesystem: %s", err.Error())
	}
	return path, nil
}

func (i *Instance) Wait(timeout time.Duration, f func() error) error {
	events := make(chan *ct.Job)
	stream, err := i.client.StreamJobEvents(os.Getenv("FLYNN_APP_ID"), events)
	if err != nil {
		return err
	}
	defer stream.Close()
	if err := f(); err != nil {
		return err
	}
	timeoutCh := time.After(timeout)
	for {
		select {
		case job, ok := <-events:
			if !ok {
				return fmt.Errorf("error streaming job events: %s", stream.Err())
			}
			if job.ID == i.job.ID && job.State == ct.JobStateDown {
				return nil
			}
		case <-timeoutCh:
			return errors.New("timed out waiting for job to stop")
		}
	}
}

func (i *Instance) Shutdown() error {
	err := i.Wait(30*time.Second, func() error {
		return i.Run("sudo poweroff", nil)
	})
	if err != nil {
		return i.Kill()
	}
	i.cleanup()
	return nil
}

func (i *Instance) Kill() error {
	defer i.cleanup()
	return i.Wait(10*time.Second, func() error {
		return i.client.DeleteJob(os.Getenv("FLYNN_APP_ID"), i.job.ID)
	})
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
	return i.run(command, s, nil, nil)
}

func (i *Instance) RunWithEnv(command string, s *Streams, env map[string]string) error {
	return i.run(command, s, env, nil)
}

func (i *Instance) RunWithTimeout(command string, s *Streams, timeout time.Duration) error {
	return i.run(command, s, nil, &timeout)
}

func (i *Instance) run(command string, s *Streams, env map[string]string, timeout *time.Duration) (err error) {
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
	for k, v := range env {
		sess.Setenv(k, v)
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to run command on %s: %s", i.IP, err)
		}
	}()
	if timeout == nil {
		return sess.Run(command)
	}
	runErr := make(chan error)
	go func() {
		runErr <- sess.Run(command)
	}()
	select {
	case err = <-runErr:
		return err
	case <-time.After(*timeout):
		return errors.New("command timed out")
	}
}
