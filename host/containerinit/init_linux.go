// +build linux

package containerinit

import "syscall"

func sethostname(hostname string) error {
	return syscall.Sethostname([]byte(hostname))
}
