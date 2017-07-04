// +build linux
package ufs

import (
	"syscall"
	"time"
)

func atime(stat *syscall.Stat_t) time.Time {
	return time.Unix(stat.Atim.Unix())
}
