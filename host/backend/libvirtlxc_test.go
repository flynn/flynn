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
	"strconv"
	"strings"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	zfs "github.com/flynn/flynn/Godeps/_workspace/src/github.com/mistifyio/go-zfs"
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

func (s *LibvirtLXCSuite) TestLibvirtContainerDevices(c *C) {
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

func (s *LibvirtLXCSuite) TestLibvirtContainerNamespaces(c *C) {
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
