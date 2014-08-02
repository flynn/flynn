package vhost

import (
	"bytes"
	"io"
	"net"
	"sync/atomic"
	"unsafe"
)

const (
	initVhostBufSize = 1024 // allocate 1 KB up front to try to avoid resizing
)

func unsafePtr(ptr *interface{}) *unsafe.Pointer {
	return (*unsafe.Pointer)(unsafe.Pointer(ptr))
}

type sharedConn struct {
	net.Conn               // the raw connection
	vhostBuf *bytes.Buffer // all of the initial data that has to be read in order to vhost a connection is saved here
}

func newShared(conn net.Conn) (*sharedConn, io.Reader) {
	c := &sharedConn{
		Conn:     conn,
		vhostBuf: bytes.NewBuffer(make([]byte, 0, initVhostBufSize)),
	}

	return c, io.TeeReader(conn, c.vhostBuf)
}

func (c *sharedConn) Read(p []byte) (n int, err error) {
	// atomic: if c.vhostBuf != nil
	if atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&c.vhostBuf))) != nil {
		n, err = c.vhostBuf.Read(p)

		// end of the request buffer
		if err == io.EOF {
			// let the request buffer get garbage collected
			// and make sure we don't read from it again
			// atomic: c.vhostBuf = nil
			atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&c.vhostBuf)), unsafe.Pointer(nil))

			// continue reading from the connection
			var n2 int
			n2, err = c.Conn.Read(p[n:])

			// update total read
			n += n2
		}
		return
	}

	return c.Conn.Read(p)
}
