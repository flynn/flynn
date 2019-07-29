package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/sse"
	"golang.org/x/net/context"
)

// Create a new sink
func (c *controllerAPI) CreateSink(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var sink ct.Sink
	if err := httphelper.DecodeJSON(req, &sink); err != nil {
		respondWithError(w, err)
		return
	}

	if err := schema.Validate(&sink); err != nil {
		respondWithError(w, err)
		return
	}

	if err := c.sinkRepo.Add(&sink); err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &sink)
}

// Get a sink
func (c *controllerAPI) GetSink(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	sink, err := c.sinkRepo.Get(params.ByName("sink_id"))
	if err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, sink)
}

// List sinks
func (c *controllerAPI) GetSinks(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		c.streamSinks(ctx, w, req)
		return
	}

	list, err := c.sinkRepo.List()
	if err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, list)
}

func (c *controllerAPI) streamSinks(ctx context.Context, w http.ResponseWriter, req *http.Request) (err error) {
	l, _ := ctxhelper.LoggerFromContext(ctx)
	ch := make(chan *ct.Sink)
	stream := sse.NewStream(w, ch, l)
	stream.Serve()
	defer func() {
		if err == nil {
			stream.Close()
		} else {
			stream.CloseWithError(err)
		}
	}()

	since, err := time.Parse(time.RFC3339Nano, req.FormValue("since"))
	if err != nil {
		return err
	}

	eventListener, err := c.maybeStartEventListener()
	if err != nil {
		l.Error("error starting event listener")
		return err
	}

	sub, err := eventListener.Subscribe("", []string{string(ct.EventTypeSink), string(ct.EventTypeSinkDeletion)}, "")
	if err != nil {
		return err
	}
	defer sub.Close()

	sinks, err := c.sinkRepo.ListSince(since)
	if err != nil {
		return err
	}
	currentUpdatedAt := since
	for _, sink := range sinks {
		select {
		case <-stream.Done:
			return nil
		case ch <- sink:
			if sink.UpdatedAt.After(currentUpdatedAt) {
				currentUpdatedAt = *sink.UpdatedAt
			}
		}
	}

	select {
	case <-stream.Done:
		return nil
	case ch <- &ct.Sink{}:
	}

	for {
		select {
		case <-stream.Done:
			return
		case event, ok := <-sub.Events:
			if !ok {
				return sub.Err
			}
			var sink ct.Sink
			if err := json.Unmarshal(event.Data, &sink); err != nil {
				l.Error("error deserializing sink event", "event.id", event.ID, "err", err)
				continue
			}
			if sink.UpdatedAt.Before(currentUpdatedAt) {
				continue
			}
			if event.ObjectType == ct.EventTypeSinkDeletion {
				sink.Config = nil
			}
			select {
			case <-stream.Done:
				return nil
			case ch <- &sink:
			}
		}
	}
}

// Delete a sink
func (c *controllerAPI) DeleteSink(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	sinkID := params.ByName("sink_id")

	sink, err := c.sinkRepo.Get(sinkID)
	if err != nil {
		respondWithError(w, err)
		return
	}

	if err = c.sinkRepo.Remove(sinkID); err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, sink)
}
