package backend

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/fsouza/go-dockerclient"
	zfs "github.com/flynn/flynn/Godeps/_workspace/src/github.com/mistifyio/go-zfs"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/mitchellh/go-ps"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/syndtr/gocapability/capability"

	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/manager"
	zfsVolume "github.com/flynn/flynn/host/volume/zfs"
	"github.com/flynn/flynn/pkg/random"
)

type ContainerSuite struct {
	id, runDir  string
	backendName string
	backend     Backend
	job         *host.Job
	pid         uint
	tty         io.ReadWriteCloser
	vman        *volumemanager.Manager
}

func (s *ContainerSuite) setup(c *C, backendName string) {
	if os.Getuid() != 0 {
		c.Skip("backend tests must be run as root")
	}

	var err error
	s.id = random.String(12)

	s.runDir, err = ioutil.TempDir("", fmt.Sprintf("flynn-test-%s.", s.id))
	c.Assert(err, IsNil)

	vdevFile := filepath.Join(s.runDir, fmt.Sprintf("flynn-test-zpool-%s.vdev", s.id))

	vman, err := volumemanager.New(
		filepath.Join(s.runDir, "volumes.bolt"),
		func() (volume.Provider, error) {
			return zfsVolume.NewProvider(&zfsVolume.ProviderConfig{
				DatasetName: fmt.Sprintf("flynn-test-zpool-%s", s.id),
				Make: &zfsVolume.MakeDev{
					BackingFilename: vdevFile,
					Size:            int64(math.Pow(2, float64(30))),
				},
				WorkingDir: filepath.Join(s.runDir, "zfs"),
			})
		},
	)
	c.Assert(err, IsNil)

	pwd, err := os.Getwd()
	c.Assert(err, IsNil)

	state := NewState("test-host", filepath.Join(s.runDir, "host-state.bolt"))

	s.backend, err = New(backendName, Config{
		State:    state,
		Manager:  vman,
		VolPath:  filepath.Join(s.runDir, "host-volumes"),
		LogPath:  filepath.Join(s.runDir, "host-logs"),
		InitPath: filepath.Join(pwd, "../bin/flynn-init"),
		Mux:      logmux.New(1000),
	})
	c.Assert(err, IsNil)

	imageURL, err := s.busyboxImageURL()
	c.Assert(err, IsNil)

	s.job = &host.Job{
		ID: s.id,
		Artifact: host.Artifact{
			URI: imageURL,
		},
		Config: host.ContainerConfig{
			Entrypoint:  []string{"/bin/sh", "-"},
			HostNetwork: true,
			Stdin:       true,
		},
	}

	attachWait := make(chan struct{})
	state.AddAttacher(s.job.ID, attachWait)

	err = s.backend.Run(s.job, nil)
	c.Assert(err, IsNil)

	stdinr, stdinw := io.Pipe()
	stdoutr, stdoutw := io.Pipe()

	s.tty = struct {
		io.WriteCloser
		io.Reader
	}{stdinw, stdoutr}

	<-attachWait
	job := state.GetJob(s.job.ID)

	attached := make(chan struct{})
	attachReq := &AttachRequest{
		Job:      job,
		Logs:     false,
		Stream:   true,
		Attached: attached,
		Stdin:    stdinr,
		Stdout:   stdoutw,
	}

	go s.backend.Attach(attachReq)
	<-attached
	close(attached)
	close(attachWait)

	activeJob := state.GetJob(s.job.ID)
	c.Assert(activeJob, Not(IsNil))
	s.pid = activeJob.Pid
}

func (s *ContainerSuite) TearDownSuite(c *C) {
	if os.Getuid() != 0 {
		return
	}

	c.Assert(s.backend.Stop(s.job.ID), IsNil)
	c.Assert(s.backend.Cleanup(), IsNil)

	zpool, err := zfs.GetZpool(fmt.Sprintf("flynn-test-zpool-%s", s.id))
	c.Assert(err, IsNil)

	err = zpool.Destroy()
	c.Assert(err, IsNil)
}

func (s *ContainerSuite) busyboxImageURL() (string, error) {
	d, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		return "", err
	}

	image, err := d.InspectImage("flynn/busybox")
	if err != nil {
		return "", err
	}

	return "https://example.com?name=flynn/busybox&id=" + image.ID, nil
}

func (s *ContainerSuite) containerCapabilities() (capability.Capabilities, error) {
	// libvirt_lxc process
	procs, err := childrenOf(int(s.pid))
	if err != nil {
		return nil, err
	}
	if len(procs) != 1 {
		return nil, fmt.Errorf("got %d child processes, want 1", len(procs))
	}

	// containerinit process
	procs, err = childrenOf(procs[0].Pid())
	if err != nil {
		return nil, err
	}
	if len(procs) != 1 {
		return nil, fmt.Errorf("got %d child processes, want 1", len(procs))
	}

	return capability.NewPid(procs[0].Pid())
}

func (s *ContainerSuite) containerCgroups() (map[string][]string, error) {
	lines, err := s.run("cat /proc/self/cgroup")
	if err != nil {
		return nil, err
	}

	cgroups := map[string][]string{}
	for _, line := range lines {
		parts := strings.Split(line, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected /proc/self/cgroup line: %q", line)
		}

		name, controllers := parts[2], strings.Split(parts[1], ",")
		cgroups[name] = append(cgroups[name], controllers...)
	}
	return cgroups, nil
}

func (s *ContainerSuite) containerDevices(dir string) (deviceSlice, error) {
	lines, err := s.run(fmt.Sprintf("ls -l %s", dir))
	if err != nil {
		return nil, err
	}
	if len(lines) < 2 {
		return nil, fmt.Errorf("no devices found in %s", dir)
	}

	// skip "total 0"
	lines = lines[1:]

	var devices deviceSlice
	for _, line := range lines {
		dev, err := parseDevice(string(line))
		if err != nil {
			return nil, err
		}

		if !dev.IsDirectory() {
			// skip directories
			devices = append(devices, dev)
		}
	}
	return devices, nil
}

type device struct {
	Name   string
	Mode   string
	Major  int
	LinkTo string
	Pipe   bool
}

func (d device) IsDirectory() bool {
	return d.Mode[0] == 'd'
}

func parseDevice(line string) (device, error) {
	var (
		name   string
		major  int
		linkTo string
		err    error
		pipe   bool
	)

	parts := strings.Fields(line)
	if line[0] == 'c' {
		major, err = strconv.Atoi(strings.TrimRight(parts[4], ","))
		name = parts[9]
	} else {
		name = parts[8]
	}

	if len(parts) >= 11 {
		linkTo = parts[10]
		if strings.Contains(linkTo, "pipe:[") {
			pipe, linkTo = true, ""
		}
	}

	return device{
		Name:   name,
		Mode:   parts[0],
		Major:  major,
		LinkTo: linkTo,
		Pipe:   pipe,
	}, err
}

type deviceSlice []device

func (s deviceSlice) get(name string) (device, bool) {
	for _, d := range s {
		if d.Name == name {
			return d, true
		}
	}
	return device{}, false
}

func (s *ContainerSuite) containerEnv() ([]string, error) {
	return s.run("strings /proc/self/environ")
}

func (s *ContainerSuite) containerMeminfo() (map[string]string, error) {
	lines, err := s.run("cat /proc/meminfo")
	if err != nil {
		return nil, err
	}

	meminfo := map[string]string{}
	for _, line := range lines {
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("unexpected /proc/meminfo line: %q", line)
		}

		meminfo[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return meminfo, nil
}

func (s *ContainerSuite) containerMounts() (mountSlice, error) {
	lines, err := s.run("cat /proc/self/mounts")
	if err != nil {
		return nil, err
	}

	mounts := mountSlice{}
	for _, line := range lines {
		parts := strings.Fields(line)
		ops := sort.StringSlice(strings.Split(parts[3], ","))
		ops.Sort()

		mounts = append(mounts, mount{
			Dev:  parts[0],
			Path: parts[1],
			Type: parts[2],
			Ops:  ops,
		})
	}
	return mounts, nil
}

type mount struct {
	Dev, Path, Type string
	Ops             []string
}

func (m mount) HasOp(op string) bool {
	return sort.SearchStrings(m.Ops, op) != len(m.Ops)
}

type mountSlice []mount

func (s mountSlice) get(path string) (mount, bool) {
	for _, m := range s {
		if m.Path == path {
			return m, true
		}
	}
	return mount{}, false
}

func (s *ContainerSuite) run(command string) ([]string, error) {
	fmt.Fprintf(s.tty, "%s ; echo EOF\n", command)

	bufr := bufio.NewReader(s.tty)
	lines := []string{}
	for {
		line, err := bufr.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == "EOF\n" {
			return lines, nil
		}

		lines = append(lines, strings.TrimSpace(line))
	}
}

func cgroupProperty(group, controller, property string) (string, error) {
	val, err := ioutil.ReadFile(filepath.Join("/sys/fs/cgroup", controller, group, property))
	return strings.TrimSpace(string(val)), err
}

func childrenOf(pid int) ([]ps.Process, error) {
	allProcs, err := ps.Processes()
	if err != nil {
		return nil, err
	}

	procs := []ps.Process{}
	for _, proc := range allProcs {
		if proc.PPid() == pid {
			procs = append(procs, proc)
		}
	}
	return procs, nil
}
