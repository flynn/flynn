package connutil

import (
	"io"
	"net"
	"net/http"
)

type CloseNotifier http.CloseNotifier

type closeNotifyConn struct {
	net.Conn

	r io.Reader

	cnc chan bool
}

// CloseNotifyConn returns a net.Conn that implements http.CloseNotifier.
// Used to detect connections closed early on the client side.
func CloseNotifyConn(conn net.Conn) net.Conn {
	pr, pw := io.Pipe()

	c := &closeNotifyConn{
		Conn: conn,
		r:    pr,
		cnc:  make(chan bool),
	}

	go func() {
		_, err := io.Copy(pw, conn)
		if err == nil {
			err = io.EOF
		}
		pw.CloseWithError(err)
		close(c.cnc)
	}()

	return c
}

// CloseNotify returns a channel that receives a single value when the client
// connection has gone away.
func (c *closeNotifyConn) CloseNotify() <-chan bool {
	return c.cnc
}

func (c *closeNotifyConn) Read(p []byte) (n int, err error) {
	return c.r.Read(p)
}
