package main

import (
	"net"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

const (
	sampleLogLine1 = "120 <40>1 2012-11-30T06:45:26+00:00 host app web.1 - - Starting process with command `bundle exec rackup config.ru -p 24405`"
	sampleLogLine2 = "79 <40>1 2012-11-30T06:45:26+00:00 host app web.2 - - 25 yay this is a message!!!\n"
)

func (s *LogAggregatorTestSuite) TestServerShutdown(c *C) {
	srv := testServer(c)
	conn, err := net.Dial("tcp", srv.SyslogAddr().String())
	c.Assert(err, IsNil)
	defer conn.Close()

	conn.Write([]byte(sampleLogLine1))
	srv.Shutdown()
}
