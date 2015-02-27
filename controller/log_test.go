package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	logaggc "github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
)

var sampleMessages = []logaggc.Message{
	{
		HostID:      "server1.flynn.local",
		JobID:       "flynn-11111111111111111111111111111111",
		Msg:         "a log message",
		ProcessType: "web",
		Source:      "app",
		Stream:      "stdout",
		Timestamp:   time.Now().UTC(),
	},
	{
		HostID:      "server2.flynn.local",
		JobID:       "flynn-22222222222222222222222222222222",
		Msg:         "another log message",
		ProcessType: "worker",
		Source:      "app",
		Stream:      "stderr",
		Timestamp:   time.Now().UTC(),
	},
}

func newFakeLogAggregator() *httptest.Server {
	f := &fakeLogAggregator{
		logs: map[string][]logaggc.Message{
			"get-app-log-test": sampleMessages,
		},
	}
	r := httprouter.New()
	r.GET("/log/:channel_id", httphelper.WrapHandler(f.serveLogs))
	return httptest.NewServer(httphelper.ContextInjector(
		"logaggregator-fake-api",
		httphelper.NewRequestLogger(r),
	))
}

type fakeLogAggregator struct {
	logs map[string][]logaggc.Message
	s    *httptest.Server
}

func (f *fakeLogAggregator) Close() {
	f.s.Close()
}

func (f *fakeLogAggregator) serveLogs(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	channelID := params.ByName("channel_id")
	allLogs := f.logs[channelID]

	vals := req.URL.Query()

	lines, _ := strconv.Atoi(vals.Get("lines"))
	if lines == 0 {
		lines = len(allLogs)
	}

	w.WriteHeader(200)

	enc := json.NewEncoder(w)
	for i := 0; i < lines; i++ {
		if err := enc.Encode(allLogs[i]); err != nil {
			panic(err)
		}
	}
}

func (s *S) TestGetAppLog(c *C) {
	s.createTestApp(c, &ct.App{Name: "get-app-log-test"})

	rc, err := s.c.GetAppLog("get-app-log-test", 0, false)
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
