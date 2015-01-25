package proxy

import (
	"bufio"
	"net"
)

// streamConn buffers reads but not writes.
type streamConn struct {
	*bufio.Reader
	net.Conn
}

func (c *streamConn) Read(b []byte) (int, error) {
	return c.Reader.Read(b)
}
