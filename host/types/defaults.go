// +build linux freebsd

package host

import "github.com/opencontainers/runc/libcontainer/configs"

// DefaultCapabilities is the default list of capabilities which are set inside
// a container, taken from:
// https://github.com/opencontainers/runc/blob/v1.0.0-rc1/libcontainer/SPEC.md#security
var DefaultCapabilities = []string{
	"CAP_NET_RAW",
	"CAP_NET_BIND_SERVICE",
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
var DefaultAllowedDevices = configs.DefaultAllowedDevices
