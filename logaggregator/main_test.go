package main

import (
	"fmt"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type LogAggregatorTestSuite struct {
	agg    *Aggregator
	api    *httptest.Server
	client *client.Client
}

var _ = Suite(&LogAggregatorTestSuite{})

func (s *LogAggregatorTestSuite) SetUpTest(c *C) {
	s.agg = NewAggregator("127.0.0.1:0")
	s.api = httptest.NewServer(apiHandler(s.agg))
	err := s.agg.Start()
	c.Assert(err, IsNil)
	s.client, err = client.New(s.api.URL)
	c.Assert(err, IsNil)
}

func (s *LogAggregatorTestSuite) TearDownTest(c *C) {
	s.api.Close()
	s.agg.Shutdown()
}

func (s *LogAggregatorTestSuite) TestAggregatorListensOnAddr(c *C) {
	ip, port, err := net.SplitHostPort(s.agg.Addr)
	c.Assert(err, IsNil)
	c.Assert(ip, Equals, "127.0.0.1")
	c.Assert(port, Not(Equals), "0")

	conn, err := net.Dial("tcp", s.agg.Addr)
	c.Assert(err, IsNil)
	defer conn.Close()
}

const (
	sampleLogLine1 = "120 <40>1 2012-11-30T06:45:26+00:00 host app web.1 - - Starting process with command `bundle exec rackup config.ru -p 24405`"
	sampleLogLine2 = "79 <40>1 2012-11-30T06:45:26+00:00 host app web.2 - - 25 yay this is a message!!!\n"
)

func (s *LogAggregatorTestSuite) TestAggregatorShutdown(c *C) {
	conn, err := net.Dial("tcp", s.agg.Addr)
	c.Assert(err, IsNil)
	defer conn.Close()

	conn.Write([]byte(sampleLogLine1))
	s.agg.Shutdown()
}

func (s *LogAggregatorTestSuite) TestAggregatorBuffersMessages(c *C) {
	// set up testing hook:
	messageReceived := make(chan struct{})
	afterMessage = func() {
		messageReceived <- struct{}{}
	}
	defer func() { afterMessage = nil }()

	conn, err := net.Dial("tcp", s.agg.Addr)
	c.Assert(err, IsNil)
	defer conn.Close()

	_, err = conn.Write([]byte(sampleLogLine1))
	c.Assert(err, IsNil)
	_, err = conn.Write([]byte(sampleLogLine2))
	c.Assert(err, IsNil)
	conn.Close()

	for i := 0; i < 2; i++ {
		<-messageReceived // wait for messages to be received
	}

	msgc := s.agg.ReadLastN("app", 0, make(chan struct{}))
	timeout := time.After(2 * time.Second)
	msgs := make([]*rfc5424.Message, 0)

Loop:
	for {
		select {
		case msg := <-msgc:
			if msg == nil {
				break Loop
			}
			msgs = append(msgs, msg)
		case <-timeout:
			c.Fatalf("timeout waiting for send on msgc")
		}
	}

	c.Assert(msgs, HasLen, 2)
	c.Assert(string(msgs[0].ProcID), Equals, "web.1")
	c.Assert(string(msgs[1].ProcID), Equals, "web.2")
}

func (s *LogAggregatorTestSuite) TestAggregatorReadLastNAndSubscribe(c *C) {
	// set up testing hook:
	messageReceived := make(chan struct{})
	afterMessage = func() {
		messageReceived <- struct{}{}
	}
	defer func() { afterMessage = nil }()

	conn, err := net.Dial("tcp", s.agg.Addr)
	c.Assert(err, IsNil)
	defer conn.Close()

	_, err = conn.Write([]byte(sampleLogLine1))
	c.Assert(err, IsNil)
	_, err = conn.Write([]byte(sampleLogLine2))
	c.Assert(err, IsNil)

	for i := 0; i < 2; i++ {
		<-messageReceived // wait for messages to be received
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msgc := s.agg.ReadLastNAndSubscribe("app", 1, ctx.Done())
	timeout := time.After(5 * time.Second)

	select {
	case msg := <-msgc:
		c.Assert(string(msg.Msg), Equals, "25 yay this is a message!!!\n")
	case <-timeout:
		c.Fatalf("timeout waiting for send on msgc")
	}

	for i := 0; i < 5; i++ {
		line := fmt.Sprintf("60 <40>1 2012-11-30T07:12:53+00:00 host app web.1 - - message %d", i)
		_, err = conn.Write([]byte(line))
		c.Assert(err, IsNil)
		<-messageReceived // wait for message to be received

		select {
		case msg := <-msgc:
			c.Assert(string(msg.Msg), Equals, fmt.Sprintf("message %d", i))
		case <-timeout:
			c.Fatalf("timeout waiting for followed message %d on msgc", i)
		}
	}
}

// TODO(bgentry): tests specifically for rfc6587Split()
