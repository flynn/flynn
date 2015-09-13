package main

import (
	"net"
	"net/http/httptest"
	"testing"

	"github.com/flynn/flynn/logaggregator/client"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type LogAggregatorTestSuite struct {
	srv    *Server
	agg    *Aggregator
	api    *httptest.Server
	client *client.Client
}

var _ = Suite(&LogAggregatorTestSuite{})

func (s *LogAggregatorTestSuite) SetUpTest(c *C) {

	var err error
	s.srv = testServer(c)
	s.agg = s.srv.Aggregator
	s.api = httptest.NewServer(s.srv.api)
	s.client, err = client.New(s.api.URL)
	c.Assert(err, IsNil)
}

func testServer(c *C) *Server {
	srvConf := ServerConfig{
		SyslogAddr:  ":0",
		ApiAddr:     ":0",
		ServiceName: "test-logaggregator",
	}

	srv, err := NewServer(srvConf)
	c.Assert(err, IsNil)
	return srv
}

func testClient(c *C, srv *Server) *client.Client {
	_, port, _ := net.SplitHostPort(srv.al.Addr().String())
	url := "http://127.0.0.1:" + port + "/"

	client, err := client.New(url)
	c.Assert(err, IsNil)

	return client
}

func (s *LogAggregatorTestSuite) TearDownTest(c *C) {
	s.api.Close()
	s.srv.Shutdown()
}

func (s *LogAggregatorTestSuite) TestAggregatorListensOnAddr(c *C) {
	go s.srv.Run()

	ip, port, err := net.SplitHostPort(s.srv.SyslogAddr().String())
	c.Assert(err, IsNil)
	c.Assert(ip, Equals, "::")
	c.Assert(port, Not(Equals), "0")

	conn, err := net.Dial("tcp", s.srv.SyslogAddr().String())
	c.Assert(err, IsNil)
	defer conn.Close()
}

// TODO(bgentry): tests specifically for rfc6587Split()
