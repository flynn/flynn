// Package mux provides a multiplexer for net.Listener.
package mux

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

// Mux represents a multiplexer for a net.Listener.
type Mux struct {
	ln   net.Listener
	once sync.Once
	wg   sync.WaitGroup

	handlers map[byte]*handler

	Timeout   time.Duration
	LogOutput io.Writer
}

// New returns a new instance of Mux.
func New(ln net.Listener) *Mux {
	return &Mux{
		ln:        ln,
		handlers:  make(map[byte]*handler),
		Timeout:   30 * time.Second,
		LogOutput: os.Stderr,
	}
}

// Close closes the underlying listener.
func (mux *Mux) Close() (err error) {
	mux.once.Do(func() {
		// Close underlying listener.
		if mux.ln != nil {
			err = mux.ln.Close()
		}

		// Wait for open connections to close and then close handlers.
		mux.wg.Wait()
		for _, h := range mux.handlers {
			h.Close()
		}
	})
	return
}

// Serve handles connections from ln and multiplexes then across registered listener.
func (mux *Mux) Serve() error {
	logger := log.New(mux.LogOutput, "", log.LstdFlags)

	for {
		// Handle incoming connections. Retry temporary errors.
		conn, err := mux.ln.Accept()
		if err, ok := err.(interface {
			Temporary() bool
		}); ok && err.Temporary() {
			logger.Printf("tcp.Mux: temporary error: %s", err)
			continue
		}

		// Other errors should close the muxer and wait for outstanding conns.
		if err != nil {
			mux.Close()
			return err
		}

		// Hand off connection to a separate goroutine.
		mux.wg.Add(1)
		go func(conn net.Conn) {
			defer mux.wg.Done()
			if err := mux.handleConn(conn); err != nil {
				conn.Close()
				logger.Printf("tcp.Mux: %s", err)
			}
		}(conn)
	}
}

func (mux *Mux) handleConn(conn net.Conn) error {
	// Wrap in a buffered connection in order to peek at the first byte.
	bufConn := newBufConn(conn)

	// Set a read deadline so connections with no data timeout.
	if err := conn.SetReadDeadline(time.Now().Add(mux.Timeout)); err != nil {
		return fmt.Errorf("set read deadline: %s", err)
	}

	// Peek at first byte from connection to determine handler.
	typ, err := bufConn.r.ReadByte()
	if err != nil {
		return fmt.Errorf("read header byte: %s", err)
	} else if err = bufConn.r.UnreadByte(); err != nil {
		return fmt.Errorf("unread header byte: %s", err)
	}

	// Reset read deadline and let the listener handle that.
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return fmt.Errorf("reset set read deadline: %s", err)
	}

	// Lookup handler.
	h := mux.handlers[typ]
	if h == nil {
		return fmt.Errorf("unregistered header byte: 0x%02x", typ)
	}

	// Hand off connection to handler.
	h.c <- bufConn
	return nil
}

// Listen returns a listener that receives connections from any byte in hdrs.
// Re-registering hdr bytes will overwrite existing handlers.
func (mux *Mux) Listen(hdrs []byte) net.Listener {
	// Create new handler.
	h := mux.handler()

	// Register each header byte.
	for _, hdr := range hdrs {
		// Create a new listener and assign it.
		mux.handlers[hdr] = h
	}

	return h
}

// handler returns a new instance of handler.
func (mux *Mux) handler() *handler {
	return &handler{
		mux: mux,
		c:   make(chan net.Conn),
	}
}

// handler is a receiver for connections received by Mux. Implements net.Listener.
type handler struct {
	mux  *Mux
	c    chan net.Conn
	once sync.Once
}

// Accept waits for and returns the next connection.
func (h *handler) Accept() (c net.Conn, err error) {
	conn, ok := <-h.c
	if !ok {
		return nil, errors.New("network connection closed")
	}
	return conn, nil
}

// Close closes the original listener.
func (h *handler) Close() error {
	h.once.Do(func() { close(h.c) })
	return nil
}

// Addr returns the address of the original listener.
func (h *handler) Addr() net.Addr { return h.mux.ln.Addr() }

// bufConn represents a buffered connection.
type bufConn struct {
	conn net.Conn
	r    *bufio.Reader
}

// newBufConn returns a new instance of bufConn.
func newBufConn(conn net.Conn) *bufConn {
	return &bufConn{
		conn: conn,
		r:    bufio.NewReader(conn),
	}
}

func (c *bufConn) Read(b []byte) (n int, err error)   { return c.r.Read(b) }
func (c *bufConn) Write(b []byte) (n int, err error)  { return c.conn.Write(b) }
func (c *bufConn) Close() error                       { return c.conn.Close() }
func (c *bufConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *bufConn) RemoteAddr() net.Addr               { return c.conn.RemoteAddr() }
func (c *bufConn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *bufConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *bufConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }
