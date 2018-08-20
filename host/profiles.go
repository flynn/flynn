package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/flynn/flynn/host/types"
	"github.com/opencontainers/runc/libcontainer/configs"
	"golang.org/x/sys/unix"
)

type jobProfileFn func(*host.Job) error

var jobProfiles = map[host.JobProfile]jobProfileFn{
	host.JobProfileZFS:  jobProfileZFS,
	host.JobProfileKVM:  jobProfileKVM,
	host.JobProfileLoop: jobProfileLoop,
}

const zfsVolMajor = 230

func jobProfileZFS(job *host.Job) error {
	zfsDev, err := loadDevice("/sys/class/misc/zfs/dev")
	if err != nil {
		return fmt.Errorf("error loading ZFS device: %s", err)
	}

	// allow the /dev/zfs and /dev/zd* zvol devices
	allowedDevices := append(*job.Config.AllowedDevices, []*configs.Device{
		{
			Path:        "/dev/zfs",
			Type:        'c',
			Major:       zfsDev.major,
			Minor:       zfsDev.minor,
			Permissions: "rwm",
		},
		{
			Type:        'b',
			Major:       zfsVolMajor,
			Minor:       configs.Wildcard,
			Permissions: "rwm",
		},
	}...)
	job.Config.AllowedDevices = &allowedDevices

	// auto create /dev/zfs
	autoCreatedDevices := append(*job.Config.AutoCreatedDevices, &configs.Device{
		Path:        "/dev/zfs",
		Type:        'c',
		Major:       zfsDev.major,
		Minor:       zfsDev.minor,
		Permissions: "rwm",
	})
	job.Config.AutoCreatedDevices = &autoCreatedDevices

	// mount /dev/zvol so the job can use symlinked zvol paths
	job.Config.Mounts = append(job.Config.Mounts, host.Mount{
		Location: "/dev/zvol",
		Target:   "/dev/zvol",
	})

	return nil
}

func jobProfileKVM(job *host.Job) error {
	kvmDev, err := loadDevice("/sys/class/misc/kvm/dev")
	if err != nil {
		return fmt.Errorf("error loading KVM device: %s", err)
	}
	tunDev, err := loadDevice("/sys/class/misc/tun/dev")
	if err != nil {
		return fmt.Errorf("error loading TUN device: %s", err)
	}

	// allow the /dev/kvm and /dev/net/tun devices
	allowedDevices := append(*job.Config.AllowedDevices, []*configs.Device{
		{
			Path:        "/dev/kvm",
			Type:        'c',
			Major:       kvmDev.major,
			Minor:       kvmDev.minor,
			Permissions: "rwm",
		},
		{
			Path:        "/dev/net/tun",
			Type:        'c',
			Major:       tunDev.major,
			Minor:       tunDev.minor,
			Permissions: "rwm",
		},
	}...)
	job.Config.AllowedDevices = &allowedDevices

	// auto create /dev/kvm and /dev/net/tun
	autoCreatedDevices := append(*job.Config.AutoCreatedDevices, []*configs.Device{
		{
			Path:        "/dev/kvm",
			Type:        'c',
			Major:       kvmDev.major,
			Minor:       kvmDev.minor,
			Permissions: "rwm",
		},
		{
			Path:        "/dev/net/tun",
			Type:        'c',
			Major:       tunDev.major,
			Minor:       tunDev.minor,
			Permissions: "rwm",
		},
	}...)
	job.Config.AutoCreatedDevices = &autoCreatedDevices

	// allow the job to create a network TAP interface
	linuxCapabilities := append(*job.Config.LinuxCapabilities, "CAP_NET_ADMIN")
	job.Config.LinuxCapabilities = &linuxCapabilities

	return nil
}

const LOOP_CTL_GET_FREE = 0x4C82

func jobProfileLoop(job *host.Job) error {
	// find an available loop device using /dev/loop-control
	f, err := os.OpenFile("/dev/loop-control", os.O_RDONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	index, err := unix.IoctlGetInt(int(f.Fd()), LOOP_CTL_GET_FREE)
	if err != nil {
		return err
	}

	// load the device
	loopDev, err := loadDevice(fmt.Sprintf("/sys/class/block/loop%d/dev", index))
	if err != nil {
		return fmt.Errorf("error loading loop device: %s", err)
	}

	// allow the loop device as /dev/loop0
	allowedDevices := append(*job.Config.AllowedDevices, []*configs.Device{
		{
			Path:        "/dev/loop0",
			Type:        'b',
			Major:       loopDev.major,
			Minor:       loopDev.minor,
			Permissions: "rwm",
		},
	}...)
	job.Config.AllowedDevices = &allowedDevices

	// auto create /dev/loop0
	autoCreatedDevices := append(*job.Config.AutoCreatedDevices, &configs.Device{
		Path:        "/dev/loop0",
		Type:        'b',
		Major:       loopDev.major,
		Minor:       loopDev.minor,
		Permissions: "rwm",
	})
	job.Config.AutoCreatedDevices = &autoCreatedDevices

	return nil
}

type device struct {
	major int64
	minor int64
}

func loadDevice(path string) (*device, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := strings.SplitN(strings.TrimSpace(string(data)), ":", 2)
	if len(s) != 2 {
		return nil, fmt.Errorf("unexpected data in %s: %q", path, data)
	}
	dev := &device{}
	dev.major, err = strconv.ParseInt(s[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error parsing device major number for %q from %q: %s", path, data, err)
	}
	dev.minor, err = strconv.ParseInt(s[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error parsing device minor number for %q from %q: %s", path, data, err)
	}
	return dev, nil
}
