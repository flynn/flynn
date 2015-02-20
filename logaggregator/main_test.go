package main

import (
	"net"
	"testing"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type LogAggregatorTestSuite struct {
	a *Aggregator
}

var _ = Suite(&LogAggregatorTestSuite{})

func (s *LogAggregatorTestSuite) SetUpTest(c *C) {
	s.a = NewAggregator("127.0.0.1:0")
	err := s.a.Start()
	c.Assert(err, IsNil)
}

func (s *LogAggregatorTestSuite) TearDownTest(c *C) {
	s.a.Shutdown()
}

func (s *LogAggregatorTestSuite) TestAggregatorListensOnAddr(c *C) {
	ip, port, err := net.SplitHostPort(s.a.Addr)
	c.Assert(err, IsNil)
	c.Assert(ip, Equals, "127.0.0.1")
	c.Assert(port, Not(Equals), "0")

	conn, err := net.Dial("tcp", s.a.Addr)
	c.Assert(err, IsNil)
	defer conn.Close()
}

const (
	sampleLogLine1 = "120 <40>1 2012-11-30T06:45:26+00:00 host app web.3 - - Starting process with command `bundle exec rackup config.ru -p 24405`"
	sampleLogLine2 = "77 <40>1 2012-11-30T06:45:26+00:00 host app web.3 - - 25 yay this is a message!!!\n"
)

func (s *LogAggregatorTestSuite) TestAggregatorShutdown(c *C) {
	conn, err := net.Dial("tcp", s.a.Addr)
	c.Assert(err, IsNil)
	defer conn.Close()

	conn.Write([]byte(sampleLogLine1))
	s.a.Shutdown()

	select {
	case <-s.a.logc:
	default:
		c.Errorf("logc was not closed")
	}
}

func (s *LogAggregatorTestSuite) TestAggregatorReceivesMessages(c *C) {
	// set up testing hook:
	messageReceived := make(chan struct{})
	afterMessage = func() {
		messageReceived <- struct{}{}
	}
	defer func() { afterMessage = nil }()

	conn, err := net.Dial("tcp", s.a.Addr)
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

	s.a.Shutdown()
}

// TODO(bgentry): tests specifically for rfc6587Split()
