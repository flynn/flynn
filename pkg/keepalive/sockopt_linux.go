// +build linux

package keepalive

import (
	"os"
	"syscall"
)

const reusePort = 0x0F
const keepaliveSecs = 180 // three minutes

func setSockopt(fd int) error {
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, reusePort, 1); err != nil {
		return os.NewSyscallError("setsockopt", err)
	}
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 1); err != nil {
		return os.NewSyscallError("setsockopt", err)
	}
	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_KEEPINTVL, keepaliveSecs); err != nil {
		return os.NewSyscallError("setsockopt", err)
	}
	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_KEEPIDLE, keepaliveSecs); err != nil {
		return os.NewSyscallError("setsockopt", err)
	}
	return nil
}
