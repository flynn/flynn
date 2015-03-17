package logmux

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/stream"
)

// serviceConn dials and watches a connection to a discoverd service. Writes
// are blocked when the service is down and during a leader change.
type serviceConn struct {
	net.Conn
	cond *sync.Cond

	closed bool

	donec  chan struct{}
	closec chan struct{}
	errc   chan error
}

func connect(discd *discoverd.Client, name string, donec chan struct{}) (*serviceConn, error) {
	srv := discd.Service(name)
	eventc := make(chan *discoverd.Event)

	stream, err := srv.Watch(eventc)
	if err != nil {
		return nil, err
	}

	sc := &serviceConn{
		cond:   &sync.Cond{L: &sync.Mutex{}},
		donec:  donec,
		closec: make(chan struct{}),
		errc:   make(chan error),
	}

	if err := sc.connect(srv); err != nil {
		grohl.Log(grohl.Data{"service": name, "status": "connect-error", "err": err.Error()})
	}

	go sc.watch(srv, eventc, stream)
	return sc, nil
}

// Write writes data to the connection.
// Write blocks while the service is unavailable. Errors from the internal
// connection are returned.
func (c *serviceConn) Write(p []byte) (int, error) {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()

	for {
		if c.closed {
			return 0, errors.New("connection closed")
		}

		if c.Conn != nil {
			n, err := c.Conn.Write(p)
			if err == nil {
				return n, nil
			}

			c.errc <- err
		}

		c.cond.Wait()
	}
}

func (c *serviceConn) watch(srv discoverd.Service, eventc <-chan *discoverd.Event, stream stream.Stream) {
	g := grohl.NewContext(grohl.Data{"at": "logmux_service_watch"})

	var (
		resetc                  = make(chan time.Time)
		reconc <-chan time.Time = resetc
	)
	defer close(resetc)

	for {
		select {
		case event, ok := <-eventc:
			if !ok {
				c.hangup()
				return
			}
			g.Log(grohl.Data{"status": "event", "event": event.Kind.String()})

			switch event.Kind {
			case discoverd.EventKindLeader:
				reconc = resetc

				if err := c.reset(); err != nil {
					g.Log(grohl.Data{"status": "error", "err": err.Error()})
				}

				if err := c.connect(srv); err != nil {
					g.Log(grohl.Data{"status": "error", "err": err.Error()})
					reconc = time.After(100 * time.Millisecond)
				}
			default:
			}
		case err := <-c.errc:
			g.Log(grohl.Data{"status": "write-error", "err": err.Error()})
			reconc = resetc

			if err := c.reset(); err != nil {
				g.Log(grohl.Data{"status": "error", "err": err.Error()})
			}

			if err := c.connect(srv); err != nil {
				g.Log(grohl.Data{"status": "error", "err": err.Error()})
				reconc = time.After(100 * time.Millisecond)
			}
		case <-reconc:
			if err := c.connect(srv); err != nil {
				g.Log(grohl.Data{"status": "reconnect-error", "err": err.Error()})
				reconc = time.After(100 * time.Millisecond)
			}
		case <-c.donec:
			if err := stream.Close(); err != nil {
				g.Log(grohl.Data{"status": "error", "err": err.Error()})
			}
			if err := c.reset(); err != nil {
				g.Log(grohl.Data{"status": "error", "err": err.Error()})
			}

			return
		case <-c.closec:
			if err := stream.Close(); err != nil {
				g.Log(grohl.Data{"status": "error", "err": err.Error()})
			}

			c.hangup()
			return
		}
	}
}

func (c *serviceConn) connect(srv discoverd.Service) error {
	ldr, err := srv.Leader()
	if err != nil {
		return err
	}

	conn, err := net.Dial("tcp", ldr.Addr)
	if err != nil {
		return err
	}

	c.cond.L.Lock()
	c.Conn = conn
	c.cond.L.Unlock()
	c.cond.Broadcast()

	return nil
}

func (c *serviceConn) reset() error {
	c.cond.L.Lock()
	conn := c.Conn
	c.Conn = nil
	c.cond.L.Unlock()

	if conn == nil {
		return nil
	}
	return conn.Close()
}

func (c *serviceConn) hangup() {
	c.reset()

	c.cond.L.Lock()
	c.closed = true
	c.cond.L.Unlock()
	c.cond.Broadcast()
}
