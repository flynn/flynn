package main

import (
	"net"
	"net/http/httptest"
	"testing"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil"
	"github.com/flynn/flynn/discoverd/testutil/etcdrunner"
	"github.com/flynn/flynn/logaggregator/client"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type LogAggregatorTestSuite struct {
	srv     *Server
	agg     *Aggregator
	api     *httptest.Server
	client  client.Client
	cleanup func()
	dc      *discoverd.Client
}

var _ = Suite(&LogAggregatorTestSuite{})

func (s *LogAggregatorTestSuite) SetUpTest(c *C) {
	etcdAddr, killEtcd := etcdrunner.RunEtcdServer(c)
	dc, killDiscoverd := testutil.BootDiscoverd(c, "", etcdAddr)
	s.cleanup = func() {
		killDiscoverd()
		killEtcd()
	}

	var err error
	s.dc = dc
	s.srv = testServer(c, dc)
	s.agg = s.srv.Aggregator
	s.api = httptest.NewServer(s.srv.api)
	s.client, err = client.New(s.api.URL)
	c.Assert(err, IsNil)
}

func testServer(c *C, dc *discoverd.Client) *Server {
	srvConf := ServerConfig{
		SyslogAddr:      ":0",
		ReplicationAddr: ":0",
		ApiAddr:         ":0",
		ServiceName:     "test-flynn-logaggregator",
		Discoverd:       dc,
	}

	srv, err := NewServer(srvConf)
	c.Assert(err, IsNil)
	return srv
}

func (s *LogAggregatorTestSuite) TearDownTest(c *C) {
	s.cleanup()
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
