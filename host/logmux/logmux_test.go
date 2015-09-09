package logmux

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
	"github.com/flynn/flynn/pkg/syslog/rfc6587"
)

// Hook gocheck up to the "go test" runner
func TestLogMux(t *testing.T) { TestingT(t) }

type S struct {
	discd   *discoverd.Client
	cleanup func()
}

var _ = Suite(&S{})

func (s *S) SetUpSuite(c *C) {
	discd, killDiscoverd := testutil.BootDiscoverd(c, "", "")

	s.discd = discd
	s.cleanup = func() {
		killDiscoverd()
	}
}

func (s *S) TearDownSuite(c *C) {
	s.cleanup()
}

func (s *S) TestLogMux(c *C) {
	l, err := net.Listen("tcp", ":0")
	c.Assert(err, IsNil)
	defer l.Close()

	addr := l.Addr().String()
	hb, err := s.discd.AddServiceAndRegister("logaggregator", addr)
	c.Assert(err, IsNil)
	defer hb.Close()

	mu := sync.Mutex{}
	srvDone := make(chan struct{})
	msgCount := 0
	handler := func(msg *rfc5424.Message) {
		mu.Lock()
		defer mu.Unlock()

		msgCount++
		if msgCount == 10000 {
			close(srvDone)
		}
	}

	go runServer(l, handler)

	lm := New(10000)
	err = lm.Connect(s.discd, "logaggregator")
	c.Assert(err, IsNil)

	config := Config{
		AppID:   "test",
		HostID:  "1234",
		JobType: "worker",
		JobID:   "567",
	}

	for i := 0; i < 100; i++ {
		pr, pw := io.Pipe()
		lm.Follow(pr, i, config)

		go func() {
			defer pw.Close()
			for j := 0; j < 100; j++ {
				fmt.Fprintf(pw, "test log entry %d\n", j)
			}
		}()
	}

	lm.Close()
	<-srvDone
}

func (s *S) TestLIFOBuffer(c *C) {
	n := 100
	l, err := net.Listen("tcp", ":0")
	c.Assert(err, IsNil)
	defer l.Close()

	mu := sync.Mutex{}
	srvDone := make(chan struct{})
	msgCount := 0
	handler := func(msg *rfc5424.Message) {
		mu.Lock()
		defer mu.Unlock()

		if !bytes.Equal(msg.Msg, []byte("retained")) {
			close(srvDone)
			c.Assert(msg.Msg, DeepEquals, []byte("retained"))
		}

		msgCount++
		if msgCount == n {
			close(srvDone)
		}
	}

	go runServer(l, handler)

	lm := New(n)
	addr := l.Addr().String()
	hb := discoverdRegister(c, s.discd, "logaggregator", addr)
	defer hb.Close()

	config := Config{
		AppID:   "test",
		HostID:  "1234",
		JobType: "worker",
		JobID:   "567",
	}

	pr, pw := io.Pipe()
	lm.Follow(pr, 1, config)

	for i := 0; i < n; i++ {
		fmt.Fprintf(pw, "retained\n")
	}
	for i := 0; i < n; i++ {
		fmt.Fprintf(pw, "dropped\n")
	}
	pw.Close()

	err = lm.Connect(s.discd, "logaggregator")
	c.Assert(err, IsNil)
	<-srvDone
}

func runServer(l net.Listener, h func(*rfc5424.Message)) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}

		go func() {
			s := bufio.NewScanner(conn)
			s.Split(rfc6587.Split)

			for s.Scan() {
				msg, err := rfc5424.Parse(s.Bytes())
				if err != nil {
					conn.Close()
					return
				}

				h(msg)
			}
			conn.Close()
		}()
	}
}

func discoverdRegister(c *C, dc *discoverd.Client, name, addr string) discoverd.Heartbeater {
	events := make(chan *discoverd.Event)
	done := make(chan struct{})

	srv := dc.Service(name)
	srv.Watch(events)

	go func() {
		defer close(done)
		for event := range events {
			if event.Kind == discoverd.EventKindUp && event.Instance.Addr == addr {
				return
			}
		}
	}()

	hb, err := dc.AddServiceAndRegister(name, addr)
	c.Assert(err, IsNil)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		c.Fatal("timed out waiting for discoverd registration")
	}

	return hb
}
