package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
	"github.com/flynn/flynn/pkg/typeconv"
)

func (s *LogAggregatorTestSuite) TestAPIGetLogWithNoResults(c *C) {
	logrc, err := s.client.GetLog("id", nil)
	c.Assert(err, IsNil)
	defer logrc.Close()

	assertAllLogsEquals(c, logrc, "")
}

func (s *LogAggregatorTestSuite) TestAPIGetLogBuffer(c *C) {
	appID := "test-app"
	msg1 := newMessageForApp(appID, "web.1", "log message 1")
	msg2 := newMessageForApp(appID, "web.2", "log message 2")
	msg3 := newMessageForApp(appID, "worker.3", "log message 3")
	msg4 := newMessageForApp(appID, "web.1", "log message 4")
	msg5 := newMessageForApp(appID, ".5", "log message 5")

	s.agg.feed(msg1)
	s.agg.feed(msg2)
	s.agg.feed(msg3)
	s.agg.feed(msg4)
	s.agg.feed(msg5)

	runtest := func(opts client.LogOpts, expected string) {
		numLines := -1
		if opts.Lines != nil {
			numLines = *opts.Lines
		}
		processType := "<nil>"
		if opts.ProcessType != nil {
			processType = *opts.ProcessType
		}
		c.Logf("Follow=%t Lines=%d JobID=%q ProcessType=%q", opts.Follow, numLines, opts.JobID, processType)
		logrc, err := s.client.GetLog(appID, &opts)
		c.Assert(err, IsNil)
		defer logrc.Close()

		assertAllLogsEquals(c, logrc, expected)
	}

	tests := []struct {
		numLogs     *int
		jobID       string
		processType *string
		expected    []*rfc5424.Message
	}{
		{
			numLogs:  typeconv.IntPtr(-1),
			expected: []*rfc5424.Message{msg1, msg2, msg3, msg4, msg5},
		},
		{
			numLogs:  typeconv.IntPtr(1),
			expected: []*rfc5424.Message{msg5},
		},
		{
			numLogs:  typeconv.IntPtr(1),
			jobID:    "3",
			expected: []*rfc5424.Message{msg3},
		},
		{
			numLogs:  typeconv.IntPtr(-1),
			jobID:    "1",
			expected: []*rfc5424.Message{msg1, msg4},
		},
		{
			numLogs:     typeconv.IntPtr(-1),
			processType: typeconv.StringPtr("web"),
			expected:    []*rfc5424.Message{msg1, msg2, msg4},
		},
		{
			numLogs:     typeconv.IntPtr(-1),
			processType: typeconv.StringPtr(""),
			expected:    []*rfc5424.Message{msg5},
		},
	}
	for _, test := range tests {
		opts := client.LogOpts{
			Follow: false,
			JobID:  test.jobID,
		}
		if test.processType != nil {
			opts.ProcessType = test.processType
		}
		if test.numLogs != nil {
			opts.Lines = test.numLogs
		}
		expected := ""
		for _, msg := range test.expected {
			expected += marshalMessage(msg)
		}
		runtest(opts, expected)
	}
}

func (s *LogAggregatorTestSuite) TestAPIGetLogFollow(c *C) {
	appID := "test-app"
	msg1 := newMessageForApp(appID, "web.1", "log message 1")
	msg2 := newMessageForApp(appID, "web.2", "log message 2")
	msg3 := newMessageForApp(appID, "web.1", "log message 3")
	msg4 := newMessageForApp(appID, "web.2", "log message 4")

	type line struct {
		text string
		err  error
	}

	s.agg.feed(msg1)
	s.agg.feed(msg2)

	nlines := 1
	logrc, err := s.client.GetLog(appID, &client.LogOpts{
		Follow: true,
		Lines:  &nlines,
	})
	c.Assert(err, IsNil)
	defer logrc.Close()

	s.agg.feed(msg3)
	s.agg.feed(msg4)

	// use a goroutine + channel so we can timeout the stdout read
	lines := make(chan line)
	go func() {
		buf := bufio.NewReader(logrc)
		for {
			text, err := buf.ReadBytes('\n')
			if err != nil {
				if err != io.EOF {
					lines <- line{"", err}
				}
				break
			}
			lines <- line{string(text), nil}
		}
	}()
	readline := func() (string, error) {
		select {
		case l := <-lines:
			if l.err != nil {
				return "", fmt.Errorf("could not read log output: %s", l.err)
			}
			return l.text, nil
		case <-time.After(1 * time.Second):
			return "", errors.New("timed out waiting for log output")
		}
	}

	expected := []string{marshalMessage(msg2), marshalMessage(msg3), marshalMessage(msg4)}
	for _, want := range expected {
		got, err := readline()
		if err != nil {
			c.Error(err)
		}
		c.Assert(err, IsNil)
		c.Assert(got, Equals, want)
	}
}

func (s *LogAggregatorTestSuite) TestNewMessageFromSyslog(c *C) {
	timestamp, err := time.Parse(time.RFC3339Nano, "2009-11-10T23:00:00.123450789Z")
	c.Assert(err, IsNil)
	m := NewMessageFromSyslog(rfc5424.NewMessage(
		&rfc5424.Header{
			Hostname:  []byte("a.b.flynn.local"),
			ProcID:    []byte("web.flynn-abcd1234"),
			MsgID:     []byte("ID1"),
			Timestamp: timestamp,
		},
		[]byte("testing message"),
	))

	c.Assert(m.HostID, Equals, "a.b.flynn.local")
	c.Assert(m.JobID, Equals, "flynn-abcd1234")
	c.Assert(m.ProcessType, Equals, "web")
	c.Assert(m.Source, Equals, "app")
	c.Assert(m.Stream, Equals, "stdout")
	c.Assert(m.Timestamp, Equals, timestamp)
}

func (s *LogAggregatorTestSuite) TestMessageMarshalJSON(c *C) {
	timestamp, err := time.Parse(time.RFC3339Nano, "2009-11-10T23:00:00.123450789Z")
	c.Assert(err, IsNil)

	m := client.Message{
		HostID:      "my.flynn.local",
		JobID:       "deadbeef1234",
		Msg:         "a log message",
		ProcessType: "web",
		Source:      "app",
		Stream:      "stderr",
		Timestamp:   timestamp,
	}
	expected := `{"host_id":"my.flynn.local","job_id":"deadbeef1234","msg":"a log message","process_type":"web","source":"app","stream":"stderr","timestamp":"2009-11-10T23:00:00.123450789Z"}`

	b, err := json.Marshal(m)
	c.Assert(err, IsNil)

	c.Assert(string(b), Equals, expected)
}

func assertAllLogsEquals(c *C, r io.Reader, expected string) {
	donec := make(chan struct{})
	go func() {
		logb, err := ioutil.ReadAll(r)
		c.Assert(err, IsNil)
		result := string(logb)
		c.Assert(result, Equals, expected)
		close(donec)
	}()

	select {
	case <-time.After(time.Second):
		c.Fatal("timeout waiting for logs")
	case <-donec:
	}
}

func newMessageForApp(appname, procID, msg string) *rfc5424.Message {
	m := rfc5424.NewMessage(
		&rfc5424.Header{
			AppName: []byte(appname),
			ProcID:  []byte(procID),
		},
		[]byte(msg),
	)
	m.StructuredData = []byte(`[flynn seq="1"]`)
	return m
}

func marshalMessage(m *rfc5424.Message) string {
	b, err := json.Marshal(NewMessageFromSyslog(m))
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}
