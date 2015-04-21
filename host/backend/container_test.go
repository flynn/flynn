package backend

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/syndtr/gocapability/capability"

	"github.com/mitchellh/go-ps"
)

type ContainerSuite struct {
	container Container
	tty       io.ReadWriteCloser
}

func (s *ContainerSuite) containerCapabilities() (capability.Capabilities, error) {
	// libvirt_lxc process
	procs, err := childrenOf(int(s.container.Pid()))
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

	devices := deviceSlice{}
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
