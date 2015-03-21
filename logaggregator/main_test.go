package main

import (
	"net"
	"net/http/httptest"
	"strings"
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
	srv    *Server
	agg    *Aggregator
	api    *httptest.Server
	client client.Client
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

const (
	sampleLogLine1 = "120 <40>1 2012-11-30T06:45:26+00:00 host app web.1 - - Starting process with command `bundle exec rackup config.ru -p 24405`"
	sampleLogLine2 = "79 <40>1 2012-11-30T06:45:26+00:00 host app web.2 - - 25 yay this is a message!!!\n"
)

var nopFilter = Filter(make(filterSlice, 0))

func (s *LogAggregatorTestSuite) TestAggregatorBuffersMessages(c *C) {
	go s.srv.Run()

	// set up testing hook:
	messageReceived := make(chan struct{})
	afterMessage = func() {
		messageReceived <- struct{}{}
	}
	defer func() { afterMessage = nil }()

	conn, err := net.Dial("tcp", s.srv.SyslogAddr().String())
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

	readAllMsgs := func(msgc <-chan *rfc5424.Message) []*rfc5424.Message {
		timeout := time.After(2 * time.Second)
		msgs := make([]*rfc5424.Message, 0)
		for {
			select {
			case msg := <-msgc:
				if msg == nil {
					return msgs
				}
				msgs = append(msgs, msg)
			case <-timeout:
				c.Fatalf("timeout waiting for send on msgc")
			}
		}
	}

	tests := []struct {
		lines           int
		filter          Filter
		expectedLen     int
		expectedProcIDs []string
	}{
		{
			lines:           -1,
			expectedLen:     2,
			expectedProcIDs: []string{"web.1", "web.2"},
		},
		{
			lines:           -1,
			filter:          filterJobID("1"),
			expectedLen:     1,
			expectedProcIDs: []string{"web.1"},
		},
		{
			lines:           1,
			filter:          filterJobID("1"),
			expectedLen:     1,
			expectedProcIDs: []string{"web.1"},
		},
	}

	for _, test := range tests {
		c.Logf("test: %+v", test)

		filter := test.filter
		if filter == nil {
			filter = nopFilter
		}

		msgc := s.agg.ReadLastN("app", test.lines, filter, make(chan struct{}))
		msgs := readAllMsgs(msgc)
		c.Assert(msgs, HasLen, test.expectedLen)
		for i, procID := range test.expectedProcIDs {
			c.Assert(string(msgs[i].ProcID), Equals, procID)
		}
	}
}

func (s *LogAggregatorTestSuite) TestAggregatorReadLastNAndSubscribe(c *C) {
	go s.srv.Run()

	runTest := func(lines int, filter Filter, expectedBefore, expectedSubMsgs, unexpectedSubMsgs []string) {
		// set up testing hook:
		messageReceived := make(chan struct{})
		afterMessage = func() {
			messageReceived <- struct{}{}
		}
		defer func() { afterMessage = nil }()

		delete(s.agg.buffers, "app") // reset the buffer
		conn, err := net.Dial("tcp", s.srv.SyslogAddr().String())
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

		if filter == nil {
			filter = nopFilter
		}

		msgc := s.agg.ReadLastNAndSubscribe("app", lines, filter, ctx.Done())
		timeout := time.After(5 * time.Second)

		for _, expectedMsg := range expectedBefore {
			select {
			case msg := <-msgc:
				c.Assert(msg, Not(IsNil))
				c.Assert(string(msg.Msg), Equals, expectedMsg)
			case <-timeout:
				c.Fatalf("timeout waiting for receive on msgc of %q", expectedMsg)
			}
		}
		select {
		case msg := <-msgc:
			c.Fatalf("unexpected message received: %+v", msg)
		default:
		}

		// make sure we skip messages we don't want
		for _, rawMsg := range unexpectedSubMsgs {
			_, err = conn.Write([]byte(rawMsg))
			c.Assert(err, IsNil)
			<-messageReceived // wait for message to be received

			select {
			case msg := <-msgc:
				c.Fatalf("received unexpected msg: %s", string(msg.Msg))
			default:
			}
		}
		// make sure we get messages we do want
		for _, rawMsg := range expectedSubMsgs {
			_, err = conn.Write([]byte(rawMsg))
			c.Assert(err, IsNil)
			<-messageReceived // wait for message to be received

			select {
			case msg := <-msgc:
				c.Assert(strings.HasSuffix(rawMsg, string(msg.Msg)), Equals, true)
			case <-timeout:
				c.Fatalf("timeout waiting for expected message on msgc: %q", rawMsg)
			}
		}
	}

	tests := []struct {
		lines             int
		filter            Filter
		expectedBefore    []string
		expectedSubMsgs   []string
		unexpectedSubMsgs []string
	}{
		{
			lines: -1,
			expectedBefore: []string{
				"Starting process with command `bundle exec rackup config.ru -p 24405`",
				"25 yay this is a message!!!\n",
			},
			expectedSubMsgs: []string{
				"60 <40>1 2012-11-30T07:12:53+00:00 host app web.1 - - message 1",
				"60 <40>1 2012-11-30T07:12:53+00:00 host app web.2 - - message 2",
			},
			unexpectedSubMsgs: []string{
				"68 <40>1 2012-11-30T07:12:53+00:00 host notapp web.1 - - not my message",
			},
		},
		{
			lines: 1,
			expectedBefore: []string{
				"25 yay this is a message!!!\n",
			},
			expectedSubMsgs: []string{
				"60 <40>1 2012-11-30T07:12:53+00:00 host app web.1 - - message 1",
				"60 <40>1 2012-11-30T07:12:53+00:00 host app web.2 - - message 2",
			},
			unexpectedSubMsgs: []string{
				"68 <40>1 2012-11-30T07:12:53+00:00 host notapp web.1 - - not my message",
			},
		},
		{
			lines:  -1,
			filter: filterJobID("2"),
			expectedBefore: []string{
				"25 yay this is a message!!!\n",
			},
			expectedSubMsgs: []string{
				"60 <40>1 2012-11-30T07:12:53+00:00 host app web.2 - - message 2",
				"68 <40>1 2012-11-30T07:12:53+00:00 host app worker.2 - - worker message",
			},
			unexpectedSubMsgs: []string{
				"60 <40>1 2012-11-30T07:12:53+00:00 host app web.1 - - message 1",
				"70 <40>1 2012-11-30T07:12:53+00:00 host app worker.1 - - worker message 1",
				"68 <40>1 2012-11-30T07:12:53+00:00 host notapp web.1 - - not my message",
			},
		},
		{
			lines:          0,
			filter:         filterProcessType("web"),
			expectedBefore: []string{},
			expectedSubMsgs: []string{
				"60 <40>1 2012-11-30T07:12:53+00:00 host app web.2 - - message 2",
				"60 <40>1 2012-11-30T07:12:53+00:00 host app web.1 - - message 1",
			},
			unexpectedSubMsgs: []string{
				"70 <40>1 2012-11-30T07:12:53+00:00 host app worker.1 - - worker message 1",
				"70 <40>1 2012-11-30T07:12:53+00:00 host app worker.2 - - worker message 2",
				"68 <40>1 2012-11-30T07:12:53+00:00 host notapp web.1 - - not my message",
			},
		},
		{
			lines:  1,
			filter: filterJobID("2"),
			expectedBefore: []string{
				"25 yay this is a message!!!\n",
			},
			expectedSubMsgs: []string{
				"60 <40>1 2012-11-30T07:12:53+00:00 host app web.2 - - message 2",
				"68 <40>1 2012-11-30T07:12:53+00:00 host app worker.2 - - worker message",
			},
			unexpectedSubMsgs: []string{
				"60 <40>1 2012-11-30T07:12:53+00:00 host app web.1 - - message 1",
				"70 <40>1 2012-11-30T07:12:53+00:00 host app worker.1 - - worker message 1",
				"68 <40>1 2012-11-30T07:12:53+00:00 host notapp web.1 - - not my message",
			},
		},
	}
	for i, test := range tests {
		c.Logf("testing num=%d lines=%d filter=%v", i, test.lines, test.filter)
		runTest(test.lines, test.filter, test.expectedBefore, test.expectedSubMsgs, test.unexpectedSubMsgs)
	}
}

// TODO(bgentry): tests specifically for rfc6587Split()
