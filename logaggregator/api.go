package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/logaggregator/snapshot"
	logagg "github.com/flynn/flynn/logaggregator/types"
	"github.com/flynn/flynn/logaggregator/utils"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"

	"github.com/julienschmidt/httprouter"
	"golang.org/x/net/context"
	"github.com/inconshreveable/log15"
)

func apiHandler(agg *Aggregator, cursors *HostCursors) http.Handler {
	api := aggregatorAPI{agg, cursors}
	r := httprouter.New()

	r.Handler("GET", status.Path, status.HealthyHandler)
	r.GET("/log/:channel_id", httphelper.WrapHandler(api.GetLog))
	r.GET("/cursors", httphelper.WrapHandler(api.GetCursors))
	r.GET("/snapshot", httphelper.WrapHandler(api.GetSnapshot))
	return httphelper.ContextInjector(
		"logaggregator-api",
		httphelper.NewRequestLogger(r),
	)
}

type aggregatorAPI struct {
	agg     *Aggregator
	cursors *HostCursors
}

func (a *aggregatorAPI) GetCursors(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	httphelper.JSON(w, 200, a.cursors.Get())
}

func (a *aggregatorAPI) GetLog(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithCancel(ctx)
	if cn, ok := w.(http.CloseNotifier); ok {
		ch := cn.CloseNotify()
		go func() {
			select {
			case <-ch:
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	defer cancel()

	params, _ := ctxhelper.ParamsFromContext(ctx)

	follow := false
	if strFollow := req.FormValue("follow"); strFollow == "true" {
		follow = true
	}

	var (
		backlog bool
		lines   int
		err     error
	)
	if strLines := req.FormValue("lines"); strLines != "" {
		if lines, err = strconv.Atoi(strLines); err != nil {
			httphelper.ValidationError(w, "lines", err.Error())
			return
		}
		if lines < 0 || lines > 10000 {
			httphelper.ValidationError(w, "lines", "lines must be an integer between 0 and 10000")
			return
		}
		backlog = lines > 0
	}

	filters := make(filterSlice, 0)
	if jobID := req.FormValue("job_id"); jobID != "" {
		filters = append(filters, filterJobID(jobID))
	}
	if processTypeVals, ok := req.Form["process_type"]; ok && len(processTypeVals) > 0 {
		val := processTypeVals[len(processTypeVals)-1]
		filters = append(filters, filterProcessType(val))
	}
	if streamTypeVals := req.FormValue("stream_types"); streamTypeVals != "" {
		vals := strings.Split(streamTypeVals, ",")
		streamTypes := make([]logagg.StreamType, len(vals))
		for i, typ := range vals {
			streamTypes[i] = logagg.StreamType(typ)
		}
		filters = append(filters, filterStreamType(streamTypes...))
	}

	iter := &Iterator{
		id:      params.ByName("channel_id"),
		follow:  follow,
		backlog: backlog,
		lines:   lines,
		filter:  filters,
		donec:   ctx.Done(),
	}

	writeMessages(ctx, w, iter.Scan(a.agg))
}

func (a *aggregatorAPI) GetSnapshot(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.flynn.logaggregator-snapshot")
	snapshot.WriteTo(a.agg.ReadAll(), w)
}

func writeMessages(ctx context.Context, w http.ResponseWriter, msgc <-chan *rfc5424.Message) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	enc := json.NewEncoder(w)
	for {
		select {
		case syslogMsg, ok := <-msgc:
			if !ok { // channel is closed / done
				return
			}
			if err := enc.Encode(NewMessageFromSyslog(syslogMsg)); err != nil {
				log15.Error("error writing msg", "err", err)
				return
			}
		case <-ticker.C:
			w.(http.Flusher).Flush()
		case <-ctx.Done():
			return
		}
	}
}

func NewMessageFromSyslog(m *rfc5424.Message) client.Message {
	processType, jobID := splitProcID(m.ProcID)
	return client.Message{
		HostID:      string(m.Hostname),
		JobID:       string(jobID),
		Msg:         string(m.Msg),
		ProcessType: string(processType),
		// TODO(bgentry): source is always "app" for now, could be router in future
		Source:    "app",
		Stream:    utils.StreamType(m),
		Timestamp: m.Timestamp,
	}
}

var procIDsep = []byte{'.'}

func splitProcID(procID []byte) (processType, jobID []byte) {
	split := bytes.SplitN(procID, procIDsep, 2)
	if len(split) < 2 {
		jobID = split[0]
	} else {
		processType = split[0]
		jobID = split[1]
	}
	return
}
