package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	logagg "github.com/flynn/flynn/logaggregator/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/sse"
	que "github.com/flynn/que-go"
	"golang.org/x/net/context"
)

type appUpdate map[string]interface{}

func (c *controllerAPI) UpdateApp(ctx context.Context, rw http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	var data appUpdate
	if err := httphelper.DecodeJSON(req, &data); err != nil {
		respondWithError(rw, err)
		return
	}

	if v, ok := data["meta"]; ok && v == nil {
		// handle {"meta": null}
		delete(data, "meta")
	}

	if err := schema.Validate(data); err != nil {
		respondWithError(rw, err)
		return
	}

	app, err := c.appRepo.Update(params.ByName("apps_id"), data)
	if err != nil {
		respondWithError(rw, err)
		return
	}
	httphelper.JSON(rw, 200, app)
}

func (c *controllerAPI) DeleteApp(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	args, err := json.Marshal(c.getApp(ctx))
	if err != nil {
		respondWithError(w, err)
		return
	}
	if err := c.que.Enqueue(&que.Job{
		Type: "app_deletion",
		Args: args,
	}); err != nil {
		respondWithError(w, err)
		return
	}
}

func (c *controllerAPI) ScheduleAppGarbageCollection(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	gc := &ct.AppGarbageCollection{AppID: c.getApp(ctx).ID}
	args, err := json.Marshal(gc)
	if err != nil {
		respondWithError(w, err)
		return
	}

	job := &que.Job{Type: "app_garbage_collection", Args: args}
	if err := c.que.Enqueue(job); err != nil {
		respondWithError(w, err)
		return
	}

	w.WriteHeader(200)
}

func (c *controllerAPI) AppLog(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithCancel(ctx)

	opts := logagg.LogOpts{
		Follow: req.FormValue("follow") == "true",
		JobID:  req.FormValue("job_id"),
	}
	if vals, ok := req.Form["process_type"]; ok && len(vals) > 0 {
		opts.ProcessType = &vals[len(vals)-1]
	}
	if streamTypeVals := req.FormValue("stream_types"); streamTypeVals != "" {
		streamTypes := strings.Split(streamTypeVals, ",")
		opts.StreamTypes = make([]logagg.StreamType, len(streamTypes))
		for i, typ := range streamTypes {
			opts.StreamTypes[i] = logagg.StreamType(typ)
		}
	}
	if strLines := req.FormValue("lines"); strLines != "" {
		lines, err := strconv.Atoi(req.FormValue("lines"))
		if err != nil {
			respondWithError(w, err)
			return
		}
		opts.Lines = &lines
	}
	rc, err := c.logaggc.GetLog(c.getApp(ctx).ID, &opts)
	if err != nil {
		respondWithError(w, err)
		return
	}

	if cn, ok := w.(http.CloseNotifier); ok {
		ch := cn.CloseNotify()
		go func() {
			select {
			case <-ch:
				rc.Close()
			case <-ctx.Done():
			}
		}()
	}
	defer cancel()
	defer rc.Close()

	if !strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		// Send headers right away if following
		if wf, ok := w.(http.Flusher); ok && opts.Follow {
			wf.Flush()
		}

		fw := httphelper.FlushWriter{Writer: w, Enabled: opts.Follow}
		io.Copy(fw, rc)
		return
	}

	ch := make(chan *ct.SSELogChunk)
	l, _ := ctxhelper.LoggerFromContext(ctx)
	s := sse.NewStream(w, ch, l)
	defer s.Close()
	s.Serve()

	msgc := make(chan *json.RawMessage)
	go func() {
		defer close(msgc)
		dec := json.NewDecoder(rc)
		for {
			var m json.RawMessage
			if err := dec.Decode(&m); err != nil {
				if err != io.EOF {
					l.Error("decoding logagg stream", err)
				}
				return
			}
			msgc <- &m
		}
	}()

	for {
		select {
		case m := <-msgc:
			if m == nil {
				ch <- &ct.SSELogChunk{Event: "eof"}
				return
			}
			// write to sse
			select {
			case ch <- &ct.SSELogChunk{Event: "message", Data: *m}:
			case <-s.Done:
				return
			case <-ctx.Done():
				return
			}
		case <-s.Done:
			return
		case <-ctx.Done():
			return
		}
	}
}
