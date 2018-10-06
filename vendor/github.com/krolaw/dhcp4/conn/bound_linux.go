// +build linux

package conn

import (
	"net"
	"os"
	"syscall"
)

func NewUDP4BoundListener(interfaceName, laddr string) (pc net.PacketConn, e error) {
	addr, err := net.ResolveUDPAddr("udp4", laddr)
	if err != nil {
		return nil, err
	}

	s, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_UDP)
	if err != nil {
		return nil, err
	}
	defer func() { // clean up if something goes wrong
		if e != nil {
			syscall.Close(s)
		}
	}()

	if err := syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return nil, err
	}
	if err := syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1); err != nil {
		return nil, err
	}
	if err := syscall.SetsockoptString(s, syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, interfaceName); err != nil {
		return nil, err
	}

	lsa := syscall.SockaddrInet4{Port: addr.Port}
	copy(lsa.Addr[:], addr.IP.To4())

	if err := syscall.Bind(s, &lsa); err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(s), "")
	defer f.Close()
	return net.FilePacketConn(f)
}
