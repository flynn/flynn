package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
)

func apiHandler(agg *Aggregator) http.Handler {
	api := aggregatorAPI{agg: agg}
	r := httprouter.New()

	r.GET("/log/:channel_id", httphelper.WrapHandler(api.GetLog))
	return httphelper.ContextInjector(
		"logaggregator-api",
		httphelper.NewRequestLogger(r),
	)
}

type aggregatorAPI struct {
	agg *Aggregator
}

func (a *aggregatorAPI) GetLog(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithCancel(ctx)
	if cn, ok := w.(http.CloseNotifier); ok {
		go func() {
			select {
			case <-cn.CloseNotify():
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	defer cancel()

	params, _ := ctxhelper.ParamsFromContext(ctx)
	channelID := params.ByName("channel_id")
	vals := req.URL.Query()

	follow := false
	if strFollow := vals.Get("follow"); strFollow == "true" {
		follow = true
	}

	lines := 0
	if strLines := vals.Get("lines"); strLines != "" {
		var err error
		lines, err = strconv.Atoi(strLines)
		if err != nil || lines < 0 || lines > 10000 {
			httphelper.ValidationError(w, "lines", "lines must be an integer between 0 and 10000")
			return
		}
	}

	w.WriteHeader(200)

	var msgc <-chan *rfc5424.Message
	if follow {
		msgc = a.agg.ReadLastNAndSubscribe(channelID, lines, ctx.Done())
		go flushLoop(w.(http.Flusher), 50*time.Millisecond, ctx.Done())
	} else {
		msgc = a.agg.ReadLastN(channelID, lines, ctx.Done())
	}

	enc := json.NewEncoder(w)
	for {
		select {
		case syslogMsg := <-msgc:
			if syslogMsg == nil { // channel is closed / done
				return
			}
			if err := enc.Encode(NewMessageFromSyslog(syslogMsg)); err != nil {
				log15.Error("error writing msg", "err", err)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func flushLoop(f http.Flusher, interval time.Duration, done <-chan struct{}) {
	for {
		select {
		case <-time.After(interval):
			f.Flush()
		case <-done:
			return
		}
	}
}

func NewMessageFromSyslog(m *rfc5424.Message) client.Message {
	processType, jobID := splitProcID(m.ProcID)
	return client.Message{
		HostID:      string(m.Hostname),
		JobID:       jobID,
		Msg:         string(m.Msg),
		ProcessType: processType,
		// TODO(bgentry): source is always "app" for now, could be router in future
		Source:    "app",
		Stream:    streamFromMessage(m),
		Timestamp: m.Timestamp,
	}
}

// TODO(bgentry): does this belong in the syslog package?
func splitProcID(procID []byte) (processType, jobID string) {
	split := bytes.Split(procID, []byte{'.'})
	if len(split) > 0 {
		processType = string(split[0])
	}
	if len(split) > 1 {
		jobID = string(split[1])
	}
	return
}

func streamFromMessage(m *rfc5424.Message) string {
	switch string(m.MsgID) {
	case "ID1":
		return "stdout"
	case "ID2":
		return "stderr"
	default:
		return "unknown"
	}
}
