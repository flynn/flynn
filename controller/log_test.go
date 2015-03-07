package main

import (
	"bufio"
	"encoding/json"
	"io"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	logaggc "github.com/flynn/flynn/logaggregator/client"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

var sampleMessages = []logaggc.Message{
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
		logs: map[string][]logaggc.Message{
			"get-app-log-test":        sampleMessages,
			"get-app-log-follow-test": sampleMessages,
		},
		subs: make(map[string]<-chan *logaggc.Message),
	}
}

type fakeLogAggregatorClient struct {
	logs map[string][]logaggc.Message
	subs map[string]<-chan *logaggc.Message
}

func (f *fakeLogAggregatorClient) GetLog(channelID string, lines int, follow bool) (io.ReadCloser, error) {
	buf := f.logs[channelID]
	if lines == 0 || lines > len(buf) {
		lines = len(buf)
	}
	pr, pw := io.Pipe()
	enc := json.NewEncoder(pw)

	go func() {
		defer pw.Close()
		for i := 0 + (len(buf) - lines); i < len(buf); i++ {
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
	appName := "get-app-log-test"
	s.createTestApp(c, &ct.App{Name: appName})

	rc, err := s.c.GetAppLog(appName, 0, false)
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

	c.Assert(msgs, DeepEquals, sampleMessages)
}

func (s *S) TestGetAppLogFollow(c *C) {
	appName := "get-app-log-follow-test"
	s.createTestApp(c, &ct.App{Name: appName})

	subc := make(chan *logaggc.Message)
	defer close(subc)
	s.flac.subs[appName] = subc
	defer func() { delete(s.flac.subs, appName) }()

	rc, err := s.c.GetAppLog(appName, 0, true)
	c.Assert(err, IsNil)
	defer rc.Close()

	msgc := make(chan *logaggc.Message)
	go func() {
		defer close(msgc)
		scanner := bufio.NewScanner(rc)
		for scanner.Scan() {
			var msg logaggc.Message
			err := json.Unmarshal(scanner.Bytes(), &msg)
			if err == io.EOF {
				return
			}
			c.Assert(err, IsNil)
			msgc <- &msg
		}
		c.Assert(scanner.Err(), IsNil)
	}()

	for i := 0; i < 2; i++ {
		select {
		case msg := <-msgc:
			c.Assert(*msg, DeepEquals, sampleMessages[i])
		case <-time.After(2 * time.Second):
			c.Fatalf("timed out waiting for buffered message %d", i)
		}
	}

	select {
	case msg := <-msgc:
		c.Fatalf("unexpected message received:", msg)
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
