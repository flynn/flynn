package main

import (
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"

	"github.com/flynn/flynn/host/types"
	"github.com/opencontainers/runc/libcontainer/configs"
)

type jobProfileFn func(*host.Job) error

var jobProfiles = map[host.JobProfile]jobProfileFn{
	host.JobProfileZFS: jobProfileZFS,
}

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
			Major:       zfsDev.major,
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
		return nil, fmt.Errorf("error parsing device major number from %q: %s", data, err)
	}
	dev.minor, err = strconv.ParseInt(s[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error parsing device minor number from %q: %s", data, err)
	}
	return dev, nil
}
