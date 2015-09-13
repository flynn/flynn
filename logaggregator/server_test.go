package main

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/logaggregator/utils"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
	"github.com/flynn/flynn/pkg/syslog/rfc6587"
)

const (
	sampleLogLine1 = `120 <40>1 2012-11-30T06:45:26+00:00 host app web.1 - [flynn seq="1"] Starting process with command bundle exec rackup config.ru -p 24405`
	sampleLogLine2 = `79 <40>1 2012-11-30T06:45:27+00:00 host app web.2 - [flynn seq="2"] 25 yay this is a message!!!` + "\n"
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

func (s *ServerTestSuite) TestServerReplication(c *C) {
	ls := testServer(c, s.dc)
	go ls.Run()
	defer ls.Shutdown()

	fs := testServer(c, s.dc)
	go fs.Run()
	defer fs.Shutdown()

	conn, err := net.Dial("tcp", ls.SyslogAddr().String())
	c.Assert(err, IsNil)
	defer conn.Close()

	lc := testClient(c, ls)
	fc := testClient(c, fs)

	zero := 0
	lrc, err := lc.GetLog("app-A", &client.LogOpts{Follow: true, Lines: &zero})
	c.Assert(err, IsNil)

	frc, err := fc.GetLog("app-A", &client.LogOpts{Follow: true, Lines: &zero})
	c.Assert(err, IsNil)

	for _, msg := range appAMessages {
		conn.Write(rfc6587.Bytes(msg))
	}

	var want, got client.Message
	ldec := json.NewDecoder(lrc)
	fdec := json.NewDecoder(frc)

	for range appAMessages {
		c.Assert(ldec.Decode(&want), IsNil)
		c.Assert(fdec.Decode(&got), IsNil)

		c.Assert(got, DeepEquals, want)
	}
}

func (s *ServerTestSuite) TestHostCursors(c *C) {
	srv := testServer(c)
	go srv.Run()
	defer srv.Shutdown()
	cl := testClient(c, srv)

	conn, err := net.Dial("tcp", srv.SyslogAddr().String())
	c.Assert(err, IsNil)
	defer conn.Close()

	assertCursors := func(expected map[string]utils.HostCursor) {
		cursors, err := cl.GetCursors()
		c.Assert(err, IsNil)
		c.Assert(cursors, DeepEquals, expected)
	}
	write := func(msg *rfc5424.Message) {
		conn.Write(rfc6587.Bytes(msg))
	}

	// write some messages
	msg1, msg2 := newSeqMessage("host1", 1, 0), newSeqMessage("host2", 1, 0)
	write(msg1)
	write(msg2)

	assertCursors(map[string]utils.HostCursor{
		"host1": utils.HostCursor{msg1.Timestamp, 1},
		"host2": utils.HostCursor{msg2.Timestamp, 1},
	})

	// test new timestamp with seq rolled over
	msg3 := newSeqMessage("host1", 1, 1)
	write(msg3)
	assertCursors(map[string]utils.HostCursor{
		"host1": utils.HostCursor{msg3.Timestamp, 1},
		"host2": utils.HostCursor{msg2.Timestamp, 1},
	})

	// test same timestamp with new seq
	msg4 := newSeqMessage("host1", 2, 1)
	write(msg4)
	assertCursors(map[string]utils.HostCursor{
		"host1": utils.HostCursor{msg4.Timestamp, 2},
		"host2": utils.HostCursor{msg2.Timestamp, 1},
	})
}

func newSeqMessage(hostname string, seq, timeDiff int) *rfc5424.Message {
	m := rfc5424.NewMessage(
		&rfc5424.Header{
			AppName:   []byte("foo"),
			ProcID:    []byte("bar"),
			Timestamp: time.Date(2013, 7, 17, 16, 43, 41, 0, time.UTC).Add(time.Duration(timeDiff) * time.Second),
			Hostname:  []byte(hostname),
		},
		[]byte("asdf"),
	)
	m.StructuredData = []byte(fmt.Sprintf(`[flynn seq="%d"]`, seq))
	return m
}
