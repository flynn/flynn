package logmux

import (
	"fmt"
	"net"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
	"github.com/flynn/flynn/pkg/syslog/rfc6587"
)

func (s *S) TestServiceConnBlockWrite(c *C) {
	l, err := net.Listen("tcp", ":0")
	c.Assert(err, IsNil)

	msgc := make(chan *rfc5424.Message)
	handler := func(msg *rfc5424.Message) {
		msgc <- msg
	}

	go runServer(l, handler)

	donec := make(chan struct{})
	defer close(donec)

	sc, err := connect(s.discd, "test-syslog", donec)
	c.Assert(err, IsNil)

	want := rfc5424.NewMessage(&rfc5424.Header{}, []byte("test message"))
	go sc.Write(rfc6587.Bytes(want))

	addr := l.Addr().String()
	_, err = s.discd.AddServiceAndRegister("test-syslog", addr)
	c.Assert(err, IsNil)

	c.Assert(<-msgc, DeepEquals, want)
}

func (s *S) TestDurableWrite(c *C) {
	l, err := net.Listen("tcp", ":0")
	c.Assert(err, IsNil)

	msgc := make(chan *rfc5424.Message)
	handler := func(msg *rfc5424.Message) {
		msgc <- msg
	}

	go runServer(l, handler)

	donec := make(chan struct{})
	defer close(donec)

	sc, err := connect(s.discd, "test-broken", donec)
	c.Assert(err, IsNil)

	addr := l.Addr().String()
	_, err = s.discd.AddServiceAndRegister("test-broken", addr)
	c.Assert(err, IsNil)

	hdr := &rfc5424.Header{}
	for i := 0; i < 10; i++ {
		want := rfc5424.NewMessage(hdr, []byte(fmt.Sprintf("line %d", i+1)))

		_, err = sc.Write(rfc6587.Bytes(want))
		c.Assert(err, IsNil)

		if i%2 == 0 {
			// break the underlying connection before the next Write
			c.Assert(sc.Conn.Close(), IsNil)
		}

		got := <-msgc
		c.Assert(want, DeepEquals, got)
	}
}

func (s *S) TestReconnect(c *C) {
	l, err := net.Listen("tcp", ":0")
	c.Assert(err, IsNil)
	err = l.Close()
	c.Assert(err, IsNil)

	msgc := make(chan *rfc5424.Message)
	handler := func(msg *rfc5424.Message) {
		msgc <- msg
	}

	addr := l.Addr().String()
	_, err = s.discd.AddServiceAndRegister("test-reconnect", addr)
	c.Assert(err, IsNil)

	donec := make(chan struct{})
	defer close(donec)

	sc, err := connect(s.discd, "test-reconnect", donec)
	c.Assert(err, IsNil)

	l, err = net.Listen("tcp", addr)
	c.Assert(err, IsNil)
	go runServer(l, handler)

	want := rfc5424.NewMessage(&rfc5424.Header{Version: 1}, []byte("test message"))
	_, err = sc.Write(rfc6587.Bytes(want))
	c.Assert(err, IsNil)

	c.Assert(<-msgc, DeepEquals, want)
}
