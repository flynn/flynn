// +build linux

package backend

import (
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	zfs "github.com/flynn/flynn/Godeps/_workspace/src/github.com/mistifyio/go-zfs"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/syndtr/gocapability/capability"

	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/manager"
	zfsVolume "github.com/flynn/flynn/host/volume/zfs"
	"github.com/flynn/flynn/pkg/random"
)

type LibvirtLXCSuite struct {
	ContainerSuite

	id, runDir string
	backend    Backend
	job        *host.Job
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

	var ok bool
	s.container, ok = s.backend.(*libvirtLXC).containers[s.id]
	c.Assert(ok, Equals, true)
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
	table := map[string]deviceSlice{
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

	for dir, wantDevs := range table {
		seenDevs := map[string]bool{}
		gotDevs, err := s.containerDevices(dir)
		c.Assert(err, IsNil)

		for _, got := range gotDevs {
			seenDevs[got.Name] = true
			gotPath := filepath.Join(dir, got.Name)

			want, ok := wantDevs.get(got.Name)
			c.Assert(ok, Equals, true)
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

		for _, want := range wantDevs {
			if !seenDevs[want.Name] {
				c.Errorf("missing device %q", filepath.Join(dir, want.Name))
			}
		}
	}
}

func (s *LibvirtLXCSuite) TestLibvirtNamespaces(c *C) {
	table := map[string]deviceSlice{
		"/proc/self/ns": deviceSlice{
			device{Name: "ipc", Mode: "lrwxrwxrwx", LinkTo: "ipc"},
			device{Name: "mnt", Mode: "lrwxrwxrwx", LinkTo: "mnt"},
			device{Name: "net", Mode: "lrwxrwxrwx", LinkTo: "net"},
			device{Name: "pid", Mode: "lrwxrwxrwx", LinkTo: "pid"},
			device{Name: "user", Mode: "lrwxrwxrwx", LinkTo: "user"},
			device{Name: "uts", Mode: "lrwxrwxrwx", LinkTo: "uts"},
		},
	}

	for dir, wantDevs := range table {
		seenDevs := map[string]bool{}
		gotDevs, err := s.containerDevices(dir)
		c.Assert(err, IsNil)

		for _, got := range gotDevs {
			seenDevs[got.Name] = true
			gotPath := filepath.Join(dir, got.Name)

			want, ok := wantDevs.get(got.Name)
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

		for _, want := range wantDevs {
			if !seenDevs[want.Name] {
				c.Errorf("missing device %q", filepath.Join(dir, want.Name))
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

	caps, err := s.containerCapabilities()
	c.Assert(err, IsNil)

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
	type properties map[string]string
	type controllers map[string]properties

	tests := map[string]controllers{
		fmt.Sprintf("/machine/%s.libvirt-lxc", s.id): controllers{
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
		"/": controllers{
			"net_cls":    nil,
			"perf_event": nil,
		},
	}

	cgroups, err := s.containerCgroups()
	c.Assert(err, IsNil)

	byGroup := map[string][]string{}
	for group, controllers := range cgroups {
		byGroup[group] = append(byGroup[group], controllers...)
	}

	for group, controllers := range tests {
		if _, ok := byGroup[group]; !ok {
			c.Errorf("missing cgroup %q", group)
			continue
		}

		for controller, properties := range controllers {
			for property, want := range properties {
				got, err := cgroupProperty(group, controller, property)
				c.Assert(err, IsNil)
				c.Assert(want, Equals, got)
			}
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
	table := []mount{
		{"devfs", "/dev", "tmpfs", []string{"rw", "mode=755"}},
		{"devpts", "/dev/pts", "devpts", []string{"rw", "mode=620", "ptmxmode=666"}},
		{"sysfs", "/sys", "sysfs", []string{"ro"}},
		{"proc", "/proc", "proc", []string{"rw"}},
		{"proc", "/proc/sys", "proc", []string{"ro"}},
		{"securityfs", "/sys/kernel/security", "securityfs", []string{"ro"}},
		{"cgroup", "/sys/fs/cgroup/cpu", "cgroup", []string{"rw", "cpu"}},
		{"cgroup", "/sys/fs/cgroup/cpuacct", "cgroup", []string{"rw", "cpuacct"}},
		{"cgroup", "/sys/fs/cgroup/cpuset", "cgroup", []string{"rw", "cpuset"}},
		{"cgroup", "/sys/fs/cgroup/memory", "cgroup", []string{"rw", "memory"}},
		{"cgroup", "/sys/fs/cgroup/devices", "cgroup", []string{"rw", "devices"}},
		{"cgroup", "/sys/fs/cgroup/freezer", "cgroup", []string{"rw", "freezer"}},
		{"cgroup", "/sys/fs/cgroup/blkio", "cgroup", []string{"rw", "blkio"}},
		{"cgroup", "/sys/fs/cgroup/net_cls", "cgroup", []string{"rw", "net_cls"}},
		{"cgroup", "/sys/fs/cgroup/perf_event", "cgroup", []string{"rw", "perf_event"}},
	}

	mounts, err := s.containerMounts()
	c.Assert(err, IsNil)

	for _, want := range table {
		got, ok := mounts.get(want.Path)
		if !ok {
			c.Errorf("missing container mount %q", want.Path)
			continue
		}

		if want.Dev != got.Dev {
			c.Errorf("want %q mount device %q, got %q", want.Path, want.Dev, got.Dev)
		}
		if want.Type != got.Type {
			c.Errorf("want %q mount type %q, got %q", want.Path, want.Type, got.Type)
		}

		for _, op := range want.Ops {
			if !got.HasOp(op) {
				c.Errorf("missing %q mount op %q", want.Path, op)
			}
		}
	}
}

func (s *LibvirtLXCSuite) TestLibvirtMeminfo(c *C) {
	table := map[string]string{
		"MemTotal":  "1017468 kB", // 1GB
		"SwapTotal": "0 kB",
	}

	meminfo, err := s.containerMeminfo()
	c.Assert(err, IsNil)

	for key, want := range table {
		got := meminfo[key]
		c.Assert(want, Equals, got)
	}
}
