// Derived from https://github.com/bradfitz/go-proxyproto

// Package proxyproto implements a net.Listener supporting HAProxy PROXY protocol.
//
// See http://www.haproxy.org/download/1.5/doc/proxy-protocol.txt for details.
package proxyproto

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
)

// prefix is the string we look for at the start of a connection
// to check if this connection is using the PROXY protocol.
var prefix = []byte("PROXY ")

// Listener wraps an underlying listener whose connections may be using the
// HAProxy PROXY Protocol (version 1). If the connection is using the protocol,
// RemoteAddr will return the correct client address.
type Listener struct {
	net.Listener
}

// conn is used to wrap an underlying connection which may be speaking the PROXY
// Protocol. If it is, the RemoteAddr() will return the address of the client
// instead of the proxy address.
type conn struct {
	net.Conn
	connBuf  []byte
	srcAddr  *net.TCPAddr
	initOnce sync.Once
}

// Accept waits for and returns the next connection to the listener.
func (p Listener) Accept() (net.Conn, error) {
	rawc, err := p.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return newConn(rawc), nil
}

func newConn(c net.Conn) *conn {
	return &conn{Conn: c}
}

// Read checks for the PROXY protocol header when doing the initial scan. If
// there is an error parsing the header, it is returned and the socket is
// closed.
func (p *conn) Read(b []byte) (int, error) {
	var err error
	p.initOnce.Do(func() { err = p.checkPrefix() })
	if err != nil {
		return 0, err
	}
	if p.connBuf != nil {
		n := copy(b, p.connBuf)
		p.connBuf = p.connBuf[n:]
		if len(p.connBuf) == 0 {
			p.connBuf = nil
		}
		if len(b) == n {
			return n, nil
		}
		readN, err := p.Conn.Read(b[n:])
		return readN + n, err
	}
	return p.Conn.Read(b)
}

// RemoteAddr returns the address of the client if the PROXY protocol is being
// used, otherwise just returns the address of the socket peer. If there is an
// error parsing the header, the address of the client is not returned, and the
// socket is closed. One implication of this is that the call could block if the
// client is slow. Using a Deadline is recommended if this is called before
// Read().
func (p *conn) RemoteAddr() net.Addr {
	p.initOnce.Do(func() { p.checkPrefix() })
	if p.srcAddr != nil {
		return p.srcAddr
	}
	return p.Conn.RemoteAddr()
}

func (p *conn) checkPrefix() error {
	buf := bufio.NewReaderSize(p.Conn, 107)

	// Incrementally check each byte of the prefix
	for i := 1; i <= len(prefix); i++ {
		inp, err := buf.Peek(i)
		if err != nil {
			if remaining := buf.Buffered(); !bytes.Equal(inp, prefix[:i]) && remaining > 0 {
				p.connBuf, _ = buf.Peek(remaining)
				return nil
			}
			return err
		}

		// Check for a prefix mis-match, quit early
		if !bytes.Equal(inp, prefix[:i]) {
			if remaining := buf.Buffered(); remaining > 0 {
				p.connBuf, _ = buf.Peek(remaining)
			}
			return nil
		}
	}

	// Read the header line
	header, err := buf.ReadString('\n')
	if err != nil {
		p.Conn.Close()
		return err
	}

	// Strip the carriage return and new line
	header = header[:len(header)-2]

	// Split on spaces, should be (PROXY <type> <src addr> <dst addr> <src port> <dst port>)
	parts := strings.Split(header, " ")
	if len(parts) != 6 {
		p.Conn.Close()
		return fmt.Errorf("proxyconn: invalid header line: %q", header)
	}

	// Verify the type is known
	if parts[1] != "TCP4" && parts[1] != "TCP6" {
		p.Conn.Close()
		return fmt.Errorf("proxyconn: unknown address type: %q", parts[1])
	}

	// Parse out the source address
	ip := net.ParseIP(parts[2])
	if ip == nil {
		p.Conn.Close()
		return fmt.Errorf("proxyconn: invalid source IP: %q", parts[2])
	}
	port, err := strconv.Atoi(parts[4])
	if err != nil {
		p.Conn.Close()
		return fmt.Errorf("proxyconn: invalid source port: %q", parts[4])
	}
	p.srcAddr = &net.TCPAddr{IP: ip, Port: port}

	if remaining := buf.Buffered(); remaining > 0 {
		p.connBuf, _ = buf.Peek(remaining)
	}

	return nil
}
