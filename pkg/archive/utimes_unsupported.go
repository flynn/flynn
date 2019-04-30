// +build !linux

package archive

import (
	"syscall"
)

func lUtimesNano(path string, ts []syscall.Timespec) error {
	return errNotSupportedPlatform
}
