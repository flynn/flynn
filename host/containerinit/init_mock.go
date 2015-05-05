// +build !linux

package containerinit

import "errors"

func sethostname(_ string) error {
	return errors.New("unsupported syscall sethostname")
}
