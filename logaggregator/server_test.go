package main

import (
	"net"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

func (s *LogAggregatorTestSuite) TestServerShutdown(c *C) {
	srv := testServer(c)
	conn, err := net.Dial("tcp", srv.SyslogAddr().String())
	c.Assert(err, IsNil)
	defer conn.Close()

	conn.Write([]byte(sampleLogLine1))
	srv.Shutdown()
}
