package main

import (
	"net"
	"net/http/httptest"
	"testing"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/logaggregator/client"
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
	sampleLogLine2 = "77 <40>1 2012-11-30T06:45:26+00:00 host app web.2 - - 25 yay this is a message!!!\n"
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

	msgs := s.agg.ReadLastN("app", 0)
	c.Assert(msgs, HasLen, 2)
	c.Assert(string(msgs[0].ProcID), Equals, "web.1")
	c.Assert(string(msgs[1].ProcID), Equals, "web.2")
	s.agg.Shutdown()
}

// TODO(bgentry): tests specifically for rfc6587Split()
