// +build linux

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
	zfs "github.com/flynn/flynn/Godeps/_workspace/src/github.com/mistifyio/go-zfs"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/syndtr/gocapability/capability"
	"github.com/mitchellh/go-ps"

	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/manager"
	zfsVolume "github.com/flynn/flynn/host/volume/zfs"
	"github.com/flynn/flynn/pkg/random"
)

type LibvirtLXCSuite struct {
	id, runDir string
	backend    Backend
	job        *host.Job
	tty        io.ReadWriteCloser
}

var _ = Suite(&LibvirtLXCSuite{})

func (s *LibvirtLXCSuite) SetUpSuite(c *C) {
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
		})
	c.Assert(err, IsNil)

	pwd, err := os.Getwd()
	c.Assert(err, IsNil)

	state := NewState("test-host", filepath.Join(s.runDir, "host-state.bolt"))

	s.backend, err = New("libvirt-lxc", Config{
		State:    state,
		Manager:  vman,
		VolPath:  filepath.Join(s.runDir, "host-volumes"),
		LogPath:  filepath.Join(s.runDir, "host-logs"),
		InitPath: filepath.Join(pwd, "../bin/flynn-init"),
		Mux:      logmux.New(1000),
	})
	c.Assert(err, IsNil)

	s.job = &host.Job{
		ID: s.id,
		Artifact: host.Artifact{
			URI: "https://registry.hub.docker.com?name=flynn/busybox&id=184af8860f22e7a87f1416bb12a32b20d0d2c142f719653d87809a6122b04663",
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
		Height:   80,
		Width:    80,
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
}

func (s *LibvirtLXCSuite) TearDownSuite(c *C) {
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

func (s *LibvirtLXCSuite) TestLibvirtDevices(c *C) {
	dirs := map[string]deviceSlice{
		"/dev": deviceSlice{
			// block devices
			device{Name: "zero", Mode: "crw-rw-rw-", Major: 1},
			device{Name: "null", Mode: "crw-rw-rw-", Major: 1},
			device{Name: "full", Mode: "crw-rw-rw-", Major: 1},
			device{Name: "random", Mode: "crw-rw-rw-", Major: 1},
			device{Name: "urandom", Mode: "crw-rw-rw-", Major: 1},
			// TTY devices
			device{Name: "ptmx", Mode: "crw-rw-rw-", Major: 5},
			device{Name: "tty", Mode: "crw-rw-rw-", Major: 5},
			// simlinked devices
			device{Name: "stdin", Mode: "lrwxrwxrwx", LinkTo: "/proc/self/fd/0"},
			device{Name: "stdout", Mode: "lrwxrwxrwx", LinkTo: "/proc/self/fd/1"},
			device{Name: "stderr", Mode: "lrwxrwxrwx", LinkTo: "/proc/self/fd/2"},
			device{Name: "fd", Mode: "lrwxrwxrwx", LinkTo: "/proc/self/fd"},
			device{Name: "console", Mode: "lrwxrwxrwx", LinkTo: "/dev/pts/0"},
			device{Name: "tty1", Mode: "lrwxrwxrwx", LinkTo: "/dev/pts/0"},
		},
		"/dev/pts": deviceSlice{
			device{Name: "ptmx", Mode: "crw-rw-rw-", Major: 5},
			device{Name: "0", Mode: "crw--w----", Major: 136}, // console
		},
		"/proc/self/fd": deviceSlice{
			device{Name: "0", Mode: "lr-x------", Pipe: true}, // stdin
			device{Name: "1", Mode: "l-wx------", Pipe: true}, // stdout
			device{Name: "2", Mode: "l-wx------", Pipe: true}, // stderr
			device{Name: "3", Mode: "lr-x------", Pipe: true}, // containerinit-rpc sock ?
		},
	}

	for dir, wants := range dirs {
		names := map[string]bool{}
		gots, err := listDevices(dir, s.tty)
		c.Assert(err, IsNil)

		for _, got := range gots {
			names[got.Name] = true
			gotPath := filepath.Join(dir, got.Name)

			want, ok := wants.get(got.Name)
			if !ok {
				c.Errorf("unexpected device %q", gotPath)
				continue
			}
			if got.Mode != want.Mode {
				c.Errorf("want %q mode %s, got %s", gotPath, want.Mode, got.Mode)
			}
			if got.Major != want.Major {
				c.Errorf("want %q major %d, got %d", gotPath, want.Major, got.Major)
			}
			if got.LinkTo != want.LinkTo {
				c.Errorf("want %q symlinked to %q, got %q", gotPath, want.LinkTo, got.LinkTo)
			}
		}

		for _, want := range wants {
			wantPath := filepath.Join(dir, want.Name)
			if !names[want.Name] {
				c.Errorf("missing device %q", wantPath)
			}
		}
	}
}

func (s *LibvirtLXCSuite) TestLibvirtNamespaces(c *C) {
	dirs := map[string]deviceSlice{
		"/proc/self/ns": deviceSlice{
			device{Name: "ipc", Mode: "lrwxrwxrwx", LinkTo: "ipc"},
			device{Name: "mnt", Mode: "lrwxrwxrwx", LinkTo: "mnt"},
			device{Name: "net", Mode: "lrwxrwxrwx", LinkTo: "net"},
			device{Name: "pid", Mode: "lrwxrwxrwx", LinkTo: "pid"},
			device{Name: "user", Mode: "lrwxrwxrwx", LinkTo: "user"},
			device{Name: "uts", Mode: "lrwxrwxrwx", LinkTo: "uts"},
		},
	}

	for dir, wants := range dirs {
		names := map[string]bool{}
		gots, err := listDevices(dir, s.tty)
		c.Assert(err, IsNil)

		for _, got := range gots {
			names[got.Name] = true
			gotPath := filepath.Join(dir, got.Name)

			want, ok := wants.get(got.Name)
			if !ok {
				c.Errorf("unexpected device %q", gotPath)
				continue
			}
			if got.Mode != want.Mode {
				c.Errorf("want %q mode %s, got %s", gotPath, want.Mode, got.Mode)
			}
			if !strings.HasPrefix(got.LinkTo, want.LinkTo+":") {
				c.Errorf("want %q link to %s inode, got %s", gotPath, want.LinkTo, got.LinkTo)
			}
		}
	}
}

func (s *LibvirtLXCSuite) TestLibvirtCapabilities(c *C) {
	table := map[capability.CapType]struct {
		Empty, Full bool

		Enabled, Disabled []capability.Cap
	}{
		capability.EFFECTIVE:   {Full: true},
		capability.PERMITTED:   {Full: true},
		capability.INHERITABLE: {Empty: true},
		capability.BOUNDING: {
			Enabled: []capability.Cap{
				capability.CAP_CHOWN,
				capability.CAP_DAC_OVERRIDE,
				capability.CAP_DAC_READ_SEARCH,
				capability.CAP_FOWNER,
				capability.CAP_FSETID,
				capability.CAP_KILL,
				capability.CAP_SETGID,
				capability.CAP_SETUID,
				capability.CAP_SETPCAP,
				capability.CAP_LINUX_IMMUTABLE,
				capability.CAP_NET_BIND_SERVICE,
				capability.CAP_NET_BROADCAST,
				capability.CAP_NET_ADMIN,
				capability.CAP_NET_RAW,
				capability.CAP_IPC_LOCK,
				capability.CAP_IPC_OWNER,
				capability.CAP_SYS_RAWIO,
				capability.CAP_SYS_CHROOT,
				capability.CAP_SYS_PTRACE,
				capability.CAP_SYS_PACCT,
				capability.CAP_SYS_ADMIN,
				capability.CAP_SYS_BOOT,
				capability.CAP_SYS_NICE,
				capability.CAP_SYS_RESOURCE,
				capability.CAP_SYS_TTY_CONFIG,
				capability.CAP_LEASE,
				capability.CAP_AUDIT_WRITE,
				capability.CAP_SETFCAP,
				capability.CAP_MAC_OVERRIDE,
				capability.CAP_SYSLOG,
				capability.CAP_WAKE_ALARM,
				capability.CAP_BLOCK_SUSPEND,
				capability.CAP_AUDIT_READ,
			},
			Disabled: []capability.Cap{
				capability.CAP_SYS_MODULE,
				capability.CAP_SYS_TIME,
				capability.CAP_MKNOD,
				capability.CAP_MAC_ADMIN,
			},
		},
	}

	caps := s.lxcContainerCaps(c)
	for capType, want := range table {
		if want.Full {
			if !caps.Full(capType) {
				c.Errorf("want %s=\"full\", got %s", capType, caps.StringCap(capType))
			}
		} else if want.Empty {
			if !caps.Empty(capType) {
				c.Errorf("want %s=\"empty\", got %s", capType, caps.StringCap(capType))
			}
		} else {
			for _, ecap := range want.Enabled {
				if !caps.Get(capType, ecap) {
					c.Errorf("missing cap %s=%q", capType, ecap)
				}
			}
			for _, dcap := range want.Disabled {
				if caps.Get(capType, dcap) {
					c.Errorf("extra cap %s=%q", capType, dcap)
				}
			}
		}
	}
}

func (s *LibvirtLXCSuite) TestLibvirtCgroups(c *C) {
	type properties map[string]interface{}

	tests := []struct {
		Group       string
		Controllers map[string]properties
	}{
		{
			Group: fmt.Sprintf("/machine/%s.libvirt-lxc", s.id),
			Controllers: map[string]properties{
				"memory": properties{
					"memory.limit_in_bytes": "1073741824", // 1GB
				},
				// defaults
				"cpuset":  nil,
				"cpu":     nil,
				"cpuacct": nil,
				"devices": nil,
				"freezer": nil,
				"blkio":   nil,
			},
		},
		{
			Group: "/",
			Controllers: map[string]properties{
				"net_cls":    nil,
				"perf_event": nil,
			},
		},
	}

	table, err := s.cgroupTable()
	c.Assert(err, IsNil)

	byGroup := map[string][]string{}
	for _, cgroup := range table {
		byGroup[cgroup.Group] = append(byGroup[cgroup.Group], cgroup.Controllers...)
	}

	seenGroups := map[string]bool{}
	for _, want := range tests {
		seenGroups[want.Group] = true

		_, ok := byGroup[want.Group]
		if !ok {
			c.Errorf("missing cgroup %q", want.Group)
			continue
		}

		for controller, properties := range want.Controllers {
			for key, wantVal := range properties {
				gotVal, err := cgroupProperty(want.Group, controller, key)
				c.Assert(err, IsNil)
				c.Assert(wantVal, Equals, gotVal)
			}
		}
	}

	for name := range byGroup {
		if _, ok := seenGroups[name]; !ok {
			c.Errorf("unexepected cgroup %q", name)
		}
	}
}

func (s *LibvirtLXCSuite) TestLibvirtEnv(c *C) {
	want := sort.StringSlice{
		fmt.Sprintf("HOSTNAME=%s", s.id),
		"HOME=/",
		"TERM=xterm",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"PWD=/",
		"container=lxc-libvirt",
	}
	want.Sort()

	got, err := s.containerEnv()
	c.Assert(err, IsNil)

	gotSlice := sort.StringSlice(got)
	gotSlice.Sort()

	c.Assert(want, DeepEquals, gotSlice)
}

func (s *LibvirtLXCSuite) TestLibvirtMounts(c *C) {
	tests := []mount{
		{"devfs", "/dev", "tmpfs", []string{"rw", "mode=755"}},
		{"devpts", "/dev/pts", "devpts", []string{"rw", "mode=620", "ptmxmode=666"}},
		{"sysfs", "/sys", "sysfs", []string{"ro"}},
		{"proc", "/proc", "proc", []string{"rw"}},
		{"proc", "/proc/sys", "proc", []string{"ro"}},
		{"securityfs", "/sys/kernel/security", "securityfs", []string{"ro"}},
	}

	gots, err := s.containerMounts()
	c.Assert(err, IsNil)

	for _, want := range tests {
		var got mount
		for i := range gots {
			if gots[i].Path == want.Path {
				got = gots[i]
				break
			}
		}
		if got.Path == "" {
			c.Errorf("missing mount %v", want)
			continue
		}

		c.Assert(want.Dev, Equals, got.Dev)
		c.Assert(want.Type, Equals, got.Type)

		sort.Strings(got.Ops)
		for _, op := range want.Ops {
			if sort.SearchStrings(got.Ops, op) == len(got.Ops) {
				c.Errorf("missing op %q", op)
			}
		}
	}
}

type mount struct {
	Dev, Path, Type string
	Ops             []string
}

func (s *LibvirtLXCSuite) containerMounts() ([]mount, error) {
	bufr := bufio.NewReader(s.tty)

	fmt.Fprintf(s.tty, "cat /proc/self/mounts ; echo EOF\n")

	mounts := []mount{}
	for {
		line, err := bufr.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == "EOF\n" {
			return mounts, nil
		}

		parts := strings.Fields(line)
		mounts = append(mounts, mount{
			Dev:  parts[0],
			Path: parts[1],
			Type: parts[2],
			Ops:  strings.Split(parts[3], ","),
		})
	}
}

func (s *LibvirtLXCSuite) containerEnv() ([]string, error) {
	bufr := bufio.NewReader(s.tty)

	fmt.Fprintf(s.tty, "/bin/strings /proc/self/environ ; echo EOF\n")

	env := []string{}
	for {
		line, err := bufr.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == "EOF\n" {
			return env, nil
		}

		env = append(env, strings.TrimSpace(line))
	}
}

func (s *LibvirtLXCSuite) lxcContainerCaps(c *C) capability.Capabilities {
	container, ok := s.backend.(*libvirtLXC).containers[s.id]
	if !ok {
		c.Fatalf("missing container for job %s", s.id)
	}

	// libvirt_lxc process
	procs, err := childrenOf(int(container.pid))
	c.Assert(err, IsNil)
	c.Assert(len(procs), Equals, 1)

	// containerinit process
	procs, err = childrenOf(procs[0].Pid())
	c.Assert(err, IsNil)
	c.Assert(len(procs), Equals, 1)

	shPid := procs[0].Pid()
	caps, err := capability.NewPid(shPid)
	c.Assert(err, IsNil)

	return caps
}

func (s *LibvirtLXCSuite) cgroupTable() ([]cgroupEntry, error) {
	bufr := bufio.NewReader(s.tty)

	fmt.Fprintf(s.tty, "cat /proc/self/cgroup ; echo EOF\n")

	cgroups := []cgroupEntry{}
	for {
		line, err := bufr.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == "EOF\n" {
			return cgroups, nil
		}

		parts := strings.Split(line, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected /proc/self/cgroup line: %q", line)
		}

		cgroups = append(cgroups, cgroupEntry{
			ID:          parts[0],
			Controllers: strings.Split(parts[1], ","),
			Group:       strings.TrimSpace(parts[2]),
		})
	}
}

type cgroupEntry struct {
	ID, Group   string
	Controllers []string
}

func cgroupProperty(group, controller, property string) (string, error) {
	val, err := ioutil.ReadFile(filepath.Join("/sys/fs/cgroup", controller, group, property))
	return strings.TrimSpace(string(val)), err
}

func listDevices(dir string, tty io.ReadWriter) (deviceSlice, error) {
	devices := deviceSlice{}
	bufr := bufio.NewReader(tty)

	fmt.Fprintf(tty, "ls -l %s ; echo EOF\n", dir)

	// read "total 0"
	if _, err := bufr.ReadString('\n'); err != nil {
		return nil, err
	}

	for {
		line, err := bufr.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line[0] == 'd' {
			// skip directories
			continue
		}
		if line == "EOF\n" {
			return devices, nil
		}

		dev, err := parseDevice(string(line))
		if err != nil {
			return nil, err
		}
		devices = append(devices, dev)
	}
}

type device struct {
	Name   string
	Mode   string
	Major  int
	LinkTo string
	Pipe   bool
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
