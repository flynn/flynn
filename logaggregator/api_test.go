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
)

func (s *LogAggregatorTestSuite) TestAPIGetLogWithNoResults(c *C) {
	logrc, err := s.client.GetLog("id", -1, false)
	c.Assert(err, IsNil)
	defer logrc.Close()

	assertAllLogsEquals(c, logrc, "")
}

func (s *LogAggregatorTestSuite) TestAPIGetLogBuffer(c *C) {
	appID := "test-app"
	msg1 := newMessageForApp(appID, "web.1", "log message 1")
	msg2 := newMessageForApp(appID, "web.2", "log message 2")
	buf := s.agg.getOrInitializeBuffer(appID)
	buf.Add(msg1)
	buf.Add(msg2)

	runtest := func(numLogs int, expected string) {
		c.Logf("numLogs=%d", numLogs)
		logrc, err := s.client.GetLog(appID, numLogs, false)
		c.Assert(err, IsNil)
		defer logrc.Close()

		assertAllLogsEquals(c, logrc, expected)
	}

	tests := []struct {
		numLogs  int
		expected string
	}{
		{
			numLogs:  -1,
			expected: marshalMessage(msg1) + marshalMessage(msg2),
		},
		{
			numLogs:  0,
			expected: "",
		},
		{
			numLogs:  1,
			expected: marshalMessage(msg2),
		},
	}
	for _, test := range tests {
		runtest(test.numLogs, test.expected)
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

	buf := s.agg.getOrInitializeBuffer(appID)
	buf.Add(msg1)
	buf.Add(msg2)

	logrc, err := s.client.GetLog(appID, 1, true)
	c.Assert(err, IsNil)
	defer logrc.Close()

	buf.Add(msg3)
	buf.Add(msg4)

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
	return rfc5424.NewMessage(
		&rfc5424.Header{
			AppName: []byte(appname),
			ProcID:  []byte(procID),
		},
		[]byte(msg),
	)
}

func marshalMessage(m *rfc5424.Message) string {
	b, err := json.Marshal(NewMessageFromSyslog(m))
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}
