// +build darwin
package ufs

import (
	"syscall"
	"time"
)

func atime(stat *syscall.Stat_t) time.Time {
	return time.Unix(stat.Atimespec.Unix())
}
