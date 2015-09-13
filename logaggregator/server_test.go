package main

import (
	"encoding/json"
	"net"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/pkg/syslog/rfc6587"
)

const (
	sampleLogLine1 = "120 <40>1 2012-11-30T06:45:26+00:00 host app web.1 - - Starting process with command `bundle exec rackup config.ru -p 24405`"
	sampleLogLine2 = "79 <40>1 2012-11-30T06:45:26+00:00 host app web.2 - - 25 yay this is a message!!!\n"
)

type ServerTestSuite struct{}

var _ = Suite(&ServerTestSuite{})

func (s *ServerTestSuite) TestServerDurability(c *C) {
	srv := testServer(c)
	go srv.Run()
	defer srv.Shutdown()

	cl := testClient(c, srv)

	conn, err := net.Dial("tcp", srv.SyslogAddr().String())
	c.Assert(err, IsNil)
	defer conn.Close()

	zero := 0
	rc, err := cl.GetLog("app-A", &client.LogOpts{Follow: true, Lines: &zero})
	c.Assert(err, IsNil)

	for _, msg := range appAMessages {
		conn.Write(rfc6587.Bytes(msg))
	}

	var got client.Message
	dec := json.NewDecoder(rc)
	for _, want := range appAMessages {
		c.Assert(dec.Decode(&got), IsNil)
		c.Assert(got.HostID, Equals, string(want.Hostname))
		c.Assert(got.Stream, Equals, streamName(want.MsgID))
		c.Assert(got.Timestamp, Equals, want.Timestamp)

		procType, jobID := splitProcID(want.ProcID)
		c.Assert(got.ProcessType, Equals, string(procType))
		c.Assert(got.JobID, Equals, string(jobID))
	}
}
