package keepalive

import (
	"bufio"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

var backlog int
var backlogOnce = &sync.Once{}

// ReusableListen returns a TCP listener with SO_REUSEPORT and keepalives
// enabled.
func ReusableListen(proto, addr string) (net.Listener, error) {
	backlogOnce.Do(func() {
		backlog = maxListenerBacklog()
	})

	saddr, typ, err := sockaddr(proto, addr)
	if err != nil {
		return nil, err
	}

	fd, err := syscall.Socket(typ, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		return nil, err
	}

	if err := setSockopt(fd); err != nil {
		return nil, err
	}

	if err := syscall.Bind(fd, saddr); err != nil {
		return nil, err
	}

	if err := syscall.Listen(fd, backlog); err != nil {
		return nil, err
	}

	f := os.NewFile(uintptr(fd), proto+":"+addr)
	l, err := net.FileListener(f)
	if err != nil {
		return nil, err
	}

	if err := f.Close(); err != nil {
		l.Close()
		return nil, err
	}

	return l, nil
}

func sockaddr(proto, addr string) (syscall.Sockaddr, int, error) {
	a, err := net.ResolveTCPAddr(proto, addr)
	if err != nil {
		return nil, -1, err
	}

	switch proto {
	case "tcp4":
		var ip [4]byte
		copy(ip[:], a.IP.To4())
		return &syscall.SockaddrInet4{Port: a.Port, Addr: ip}, syscall.AF_INET, nil
	case "tcp6":
		var ip [16]byte
		copy(ip[:], a.IP.To16())
		// TODO: this does not set the zone
		return &syscall.SockaddrInet6{Port: a.Port, Addr: ip}, syscall.AF_INET6, nil
	default:
		return nil, -1, errors.New("reuseport: only tcp4 and tcp6 are supported")
	}
}

func maxListenerBacklog() int {
	f, err := os.Open("/proc/sys/net/core/somaxconn")
	if err != nil {
		return syscall.SOMAXCONN
	}
	defer f.Close()
	r := bufio.NewReader(f)
	l, err := r.ReadString('\n')
	if err != nil {
		return syscall.SOMAXCONN
	}
	fs := strings.Fields(l)
	if len(fs) < 1 {
		return syscall.SOMAXCONN
	}
	n, err := strconv.Atoi(fs[0])
	if err != nil || n == 0 {
		return syscall.SOMAXCONN
	}
	// Linux stores the backlog in a uint16.
	// Truncate number to avoid wrapping.
	// See Go issue 5030.
	if n > 1<<16-1 {
		n = 1<<16 - 1
	}
	return n
}
