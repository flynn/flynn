// Copyright (C) 2013 Max Riveiro
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
// The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

// Package reuseport provides a function that returns a net.Listener powered by a net.FileListener with a SO_REUSEPORT option set to the socket.
package reuseport

import (
	"errors"
	"net"
	"os"
	"strconv"
	"syscall"
)

const (
	unsupportedProtoError = "Only tcp4 and tcp6 are supported"
	filePrefix            = "port."
)

// getSockaddr parses protocol and address and returns implementor syscall.Sockaddr: syscall.SockaddrInet4 or syscall.SockaddrInet6.
func getSockaddr(proto, addr string) (sa syscall.Sockaddr, soType int, err error) {
	var (
		addr4 [4]byte
		addr6 [16]byte
		tip   *net.TCPAddr
		uip   *net.UDPAddr
	)

	switch proto {
	default:
		return nil, -1, errors.New(unsupportedProtoError)
	case "tcp4":
		tip, err = net.ResolveTCPAddr(proto, addr)
		if err != nil {
			return nil, -1, err
		}

		if tip.IP != nil {
			copy(addr4[:], tip.IP[12:16]) // copy last 4 bytes of slice to array
		}
		return &syscall.SockaddrInet4{Port: tip.Port, Addr: addr4}, syscall.AF_INET, nil
	case "tcp6":
		tip, err = net.ResolveTCPAddr(proto, addr)
		if err != nil {
			return nil, -1, err
		}

		if tip.IP != nil {
			copy(addr6[:], tip.IP) // copy last 4 bytes of slice to array
		}
		return &syscall.SockaddrInet6{Port: tip.Port, Addr: addr6}, syscall.AF_INET6, nil
	case "udp4":
		uip, err = net.ResolveUDPAddr(proto, addr)
		if err != nil {
			return nil, -1, err
		}

		if uip.IP != nil {
			copy(addr4[:], uip.IP[12:16]) // copy last 4 bytes of slice to array
		}
		return &syscall.SockaddrInet4{Port: uip.Port, Addr: addr4}, syscall.AF_INET, nil
	case "udp6":
		uip, err = net.ResolveUDPAddr(proto, addr)
		if err != nil {
			return nil, -1, err
		}

		if uip.IP != nil {
			copy(addr6[:], uip.IP) // copy all bytes of slice to array
		}
		return &syscall.SockaddrInet6{Port: uip.Port, Addr: addr6}, syscall.AF_INET6, nil
	}
}

// NewReusablePortListener returns net.FileListener that created from a file discriptor for a socket with SO_REUSEPORT option.
func NewReusablePortListener(proto, addr string) (l net.Listener, err error) {
	var (
		soType, fd int
		file       *os.File
		sockaddr   syscall.Sockaddr
	)

	if sockaddr, soType, err = getSockaddr(proto, addr); err != nil {
		return nil, err
	}

	if fd, err = syscall.Socket(soType, syscall.SOCK_STREAM, syscall.IPPROTO_TCP); err != nil {
		return nil, err
	}

	if err = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, reusePort, 1); err != nil {
		return nil, err
	}

	if err = syscall.Bind(fd, sockaddr); err != nil {
		return nil, err
	}

	// Set backlog size to the maximum
	if err = syscall.Listen(fd, syscall.SOMAXCONN); err != nil {
		return nil, err
	}

	// File Name get be nil
	file = os.NewFile(uintptr(fd), filePrefix+strconv.Itoa(os.Getpid()))
	if l, err = net.FileListener(file); err != nil {
		return nil, err
	}

	if err = file.Close(); err != nil {
		return nil, err
	}

	return l, err
}

// NewReusablePortPacketConn returns net.FileListener that created from a file discriptor for a socket with SO_REUSEPORT option.
func NewReusablePortPacketConn(proto, addr string) (l net.PacketConn, err error) {
	var (
		soType, fd int
		file       *os.File
		sockaddr   syscall.Sockaddr
	)

	if sockaddr, soType, err = getSockaddr(proto, addr); err != nil {
		return nil, err
	}

	if fd, err = syscall.Socket(soType, syscall.SOCK_DGRAM, syscall.IPPROTO_UDP); err != nil {
		return nil, err
	}

	if err = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, reusePort, 1); err != nil {
		return nil, err
	}

	if err = syscall.Bind(fd, sockaddr); err != nil {
		return nil, err
	}

	// File Name get be nil
	file = os.NewFile(uintptr(fd), filePrefix+strconv.Itoa(os.Getpid()))
	if l, err = net.FilePacketConn(file); err != nil {
		return nil, err
	}

	if err = file.Close(); err != nil {
		return nil, err
	}

	return l, err
}
