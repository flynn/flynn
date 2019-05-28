// +build linux

package host

import "github.com/opencontainers/runc/libcontainer/configs"

// DefaultCapabilities is the default list of capabilities which are set inside
// a container, taken from:
// https://github.com/opencontainers/runc/blob/v1.0.0-rc8/libcontainer/SPEC.md#security
var DefaultCapabilities = []string{
	"CAP_NET_RAW",
	"CAP_NET_BIND_SERVICE",
	"CAP_AUDIT_READ",
	"CAP_AUDIT_WRITE",
	"CAP_DAC_OVERRIDE",
	"CAP_SETFCAP",
	"CAP_SETPCAP",
	"CAP_SETGID",
	"CAP_SETUID",
	"CAP_MKNOD",
	"CAP_CHOWN",
	"CAP_FOWNER",
	"CAP_FSETID",
	"CAP_KILL",
	"CAP_SYS_CHROOT",
}

// DefaultAllowedDevices is the default list of devices containers are allowed
// to access
var DefaultAllowedDevices = fromConfigDevices(configs.DefaultAllowedDevices)

// DefaultAutoCreatedDevices is the default list of devices created inside
// containers
var DefaultAutoCreatedDevices = fromConfigDevices(configs.DefaultAllowedDevices)

func (d *Device) Config() *configs.Device {
	return &configs.Device{
		Type:        d.Type,
		Path:        d.Path,
		Major:       d.Major,
		Minor:       d.Minor,
		Permissions: d.Permissions,
		FileMode:    d.FileMode,
		Uid:         d.Uid,
		Gid:         d.Gid,
		Allow:       d.Allow,
	}
}

func fromConfigDevices(ds []*configs.Device) []*Device {
	res := make([]*Device, len(ds))
	for i, d := range ds {
		res[i] = &Device{
			Type:        d.Type,
			Path:        d.Path,
			Major:       d.Major,
			Minor:       d.Minor,
			Permissions: d.Permissions,
			FileMode:    d.FileMode,
			Uid:         d.Uid,
			Gid:         d.Gid,
			Allow:       d.Allow,
		}
	}
	return res
}

func ConfigDevices(ds []*Device) []*configs.Device {
	res := make([]*configs.Device, len(ds))
	for i, d := range ds {
		res[i] = d.Config()
	}
	return res
}
