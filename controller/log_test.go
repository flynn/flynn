package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"sync"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	logaggc "github.com/flynn/flynn/logaggregator/client"
	logagg "github.com/flynn/flynn/logaggregator/types"
	"github.com/flynn/flynn/pkg/typeconv"

	. "github.com/flynn/go-check"
)

var sampleMessages = []logaggc.Message{
	{
		HostID:      "server1.flynn.local",
		JobID:       "00000000000000000000000000000000",
		Msg:         "a log message from a job with an empty type",
		ProcessType: "",
		Source:      "app",
		Stream:      "stdout",
		Timestamp:   time.Unix(1425688100, 000000000).UTC(),
	},
	{
		HostID:      "server1.flynn.local",
		JobID:       "11111111111111111111111111111111",
		Msg:         "a stdout log message",
		ProcessType: "web",
		Source:      "app",
		Stream:      "stdout",
		Timestamp:   time.Unix(1425688201, 111111111).UTC(),
	},
	{
		HostID:      "server2.flynn.local",
		JobID:       "22222222222222222222222222222222",
		Msg:         "a stderr log message",
		ProcessType: "worker",
		Source:      "app",
		Stream:      "stderr",
		Timestamp:   time.Unix(1425688521, 222222222).UTC(),
	},
}

func newFakeLogAggregatorClient() *fakeLogAggregatorClient {
	return &fakeLogAggregatorClient{
		logs: make(map[string][]logaggc.Message),
		subs: make(map[string]<-chan *logaggc.Message),
	}
}

type fakeLogAggregatorClient struct {
	logs map[string][]logaggc.Message
	subs map[string]<-chan *logaggc.Message
}

func (f *fakeLogAggregatorClient) GetLog(channelID string, options *logagg.LogOpts) (io.ReadCloser, error) {
	buf, ok := f.logs[channelID]
	if !ok {
		buf = sampleMessages
		f.logs[channelID] = buf
	}
	lines := len(buf)
	follow := false
	jobID, processType := "", ""
	filterProcType := false

	if options != nil {
		opts := *options
		if opts.Lines != nil && *opts.Lines >= 0 {
			lines = *opts.Lines
		}
		if opts.ProcessType != nil {
			filterProcType = true
			processType = *opts.ProcessType
		}
		follow = opts.Follow
		jobID = opts.JobID
	}
	if lines > len(buf) {
		lines = len(buf)
	}

	pr, pw := io.Pipe()
	enc := json.NewEncoder(pw)

	go func() {
		defer pw.Close()
		for i := 0 + (len(buf) - lines); i < len(buf); i++ {
			if jobID != "" && jobID != buf[i].JobID {
				continue
			}
			if filterProcType && processType != buf[i].ProcessType {
				continue
			}
			if err := enc.Encode(buf[i]); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		if subc, ok := f.subs[channelID]; ok && follow {
			for msg := range subc {
				if err := enc.Encode(msg); err != nil {
					pw.CloseWithError(err)
					return
				}
			}
		}
	}()
	return pr, nil
}

func (s *S) TestGetAppLog(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "get-app-log-test"})

	tests := []struct {
		opts     *logagg.LogOpts
		expected []logaggc.Message
	}{
		{
			expected: sampleMessages,
		},
		{
			opts:     &logagg.LogOpts{Lines: typeconv.IntPtr(1)},
			expected: sampleMessages[2:],
		},
		{
			opts:     &logagg.LogOpts{ProcessType: typeconv.StringPtr("web")},
			expected: sampleMessages[1:2],
		},
		{
			opts:     &logagg.LogOpts{ProcessType: typeconv.StringPtr("")},
			expected: sampleMessages[:1],
		},
		{
			opts:     &logagg.LogOpts{JobID: "11111111111111111111111111111111"},
			expected: sampleMessages[1:2],
		},
	}

	for _, test := range tests {
		opts := test.opts
		if opts != nil {
			numLines := ""
			if opts.Lines != nil {
				numLines = strconv.Itoa(*opts.Lines)
			}
			processType := "<nil>"
			if opts.ProcessType != nil {
				processType = *opts.ProcessType
			}
			c.Logf("Follow=%t Lines=%q JobID=%q ProcessType=%q", opts.Follow, numLines, opts.JobID, processType)
		}
		rc, err := s.c.GetAppLog(app.Name, opts)
		c.Assert(err, IsNil)
		defer rc.Close()

		msgs := make([]logaggc.Message, 0)
		dec := json.NewDecoder(rc)
		for {
			var msg logaggc.Message
			err := dec.Decode(&msg)
			if err == io.EOF {
				break
			}
			c.Assert(err, IsNil)
			msgs = append(msgs, msg)
		}

		c.Assert(msgs, DeepEquals, test.expected)
	}
}

func (s *S) TestGetAppLogFollow(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "get-app-log-follow-test"})

	subc := make(chan *logaggc.Message)
	defer close(subc)
	s.flac.subs[app.ID] = subc
	defer func() { delete(s.flac.subs, app.ID) }()

	rc, err := s.c.GetAppLog(app.Name, &logagg.LogOpts{
		Lines:  nil,
		Follow: true,
	})
	c.Assert(err, IsNil)
	defer rc.Close()

	msgc := make(chan *logaggc.Message)
	msgErr := make(chan error)
	go func() {
		defer close(msgc)
		scanner := bufio.NewScanner(rc)
		for scanner.Scan() {
			var msg logaggc.Message
			err := json.Unmarshal(scanner.Bytes(), &msg)
			if err == io.EOF {
				return
			} else if err != nil {
				msgErr <- err
				return
			}
			msgc <- &msg
		}
		msgErr <- scanner.Err()
	}()

	for i := 0; i < 3; i++ {
		select {
		case msg := <-msgc:
			c.Assert(*msg, DeepEquals, sampleMessages[i])
		case err := <-msgErr:
			c.Assert(err, IsNil)
		case <-time.After(2 * time.Second):
			c.Fatalf("timed out waiting for buffered message %d", i)
		}
	}

	select {
	case msg := <-msgc:
		c.Fatal("unexpected message received: ", msg)
	default:
	}

	newMsg := &logaggc.Message{
		HostID:      "server3.flynn.local",
		JobID:       "33333333333333333333333333333333",
		Msg:         "another stdout log message",
		ProcessType: "web",
		Source:      "app",
		Stream:      "stdout",
		Timestamp:   time.Unix(1425688533, 333333333).UTC(),
	}
	go func() { subc <- newMsg }()
	select {
	case msg := <-msgc:
		c.Assert(msg, DeepEquals, newMsg)
	case <-time.After(2 * time.Second):
		c.Fatalf("timed out waiting for followed message")
	}
}

func (s *S) TestGetAppLogSSE(c *C) {
	appName := "get-app-log-sse-test"
	s.createTestApp(c, &ct.App{Name: appName})

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/apps/%s/log", s.srv.URL, appName), nil)
	c.Assert(err, IsNil)
	req.SetBasicAuth("", authKey)
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	c.Assert(err, IsNil)

	var buf bytes.Buffer
	_, err = buf.ReadFrom(res.Body)
	res.Body.Close()
	c.Assert(err, IsNil)

	expected := `data: {"event":"message","data":{"host_id":"server1.flynn.local","job_id":"00000000000000000000000000000000","msg":"a log message from a job with an empty type","source":"app","stream":"stdout","timestamp":"2015-03-07T00:28:20Z"}}` +
		"\n\n" +
		`data: {"event":"message","data":{"host_id":"server1.flynn.local","job_id":"11111111111111111111111111111111","msg":"a stdout log message","process_type":"web","source":"app","stream":"stdout","timestamp":"2015-03-07T00:30:01.111111111Z"}}` +
		"\n\n" +
		`data: {"event":"message","data":{"host_id":"server2.flynn.local","job_id":"22222222222222222222222222222222","msg":"a stderr log message","process_type":"worker","source":"app","stream":"stderr","timestamp":"2015-03-07T00:35:21.222222222Z"}}` +
		"\n\n" +
		`data: {"event":"eof"}` + "\n\n"

	c.Assert(buf.String(), Equals, expected)
}

func (s *S) TestGetAppLogSSEFollow(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "get-app-log-sse-follow-test"})

	done := make(chan struct{})
	defer close(done)
	subc := make(chan *logaggc.Message)
	var closeSubc sync.Once
	defer closeSubc.Do(func() { close(subc) })
	s.flac.subs[app.ID] = subc
	defer func() { delete(s.flac.subs, app.ID) }()

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/apps/%s/log?follow=true", s.srv.URL, app.Name), nil)
	c.Assert(err, IsNil)
	req.SetBasicAuth("", authKey)
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	c.Assert(err, IsNil)
	defer res.Body.Close()

	newMsg := &logaggc.Message{
		HostID:      "server3.flynn.local",
		JobID:       "33333333333333333333333333333333",
		Msg:         "another stdout log message",
		ProcessType: "web",
		Source:      "app",
		Stream:      "stdout",
		Timestamp:   time.Unix(1425688533, 333333333).UTC(),
	}
	go func() {
		select {
		case subc <- newMsg:
			defer closeSubc.Do(func() { close(subc) })
		case <-done:
		}
	}()

	resc := make(chan []byte)
	go func() {
		res, _ := ioutil.ReadAll(res.Body)
		select {
		case resc <- res:
		case <-done:
		}
	}()

	expected := `data: {"event":"message","data":{"host_id":"server1.flynn.local","job_id":"00000000000000000000000000000000","msg":"a log message from a job with an empty type","source":"app","stream":"stdout","timestamp":"2015-03-07T00:28:20Z"}}` +
		"\n\n" +
		`data: {"event":"message","data":{"host_id":"server1.flynn.local","job_id":"11111111111111111111111111111111","msg":"a stdout log message","process_type":"web","source":"app","stream":"stdout","timestamp":"2015-03-07T00:30:01.111111111Z"}}` +
		"\n\n" +
		`data: {"event":"message","data":{"host_id":"server2.flynn.local","job_id":"22222222222222222222222222222222","msg":"a stderr log message","process_type":"worker","source":"app","stream":"stderr","timestamp":"2015-03-07T00:35:21.222222222Z"}}` +
		"\n\n" +
		`data: {"event":"message","data":{"host_id":"server3.flynn.local","job_id":"33333333333333333333333333333333","msg":"another stdout log message","process_type":"web","source":"app","stream":"stdout","timestamp":"2015-03-07T00:35:33.333333333Z"}}` +
		"\n\n" +
		`data: {"event":"eof"}` + "\n\n"

	select {
	case res := <-resc:
		c.Assert(string(res), Equals, expected)

	case <-time.After(5 * time.Second):
		c.Fatalf("timed out waiting for response")
	}

}
