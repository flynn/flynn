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

func (c *controllerAPI) GetVolumes(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		c.streamVolumes(ctx, w, req)
		return
	}

	list, err := c.volumeRepo.List()
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, list)
}

func (c *controllerAPI) GetAppVolumes(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	list, err := c.volumeRepo.AppList(c.getApp(ctx).ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, list)
}

func (c *controllerAPI) GetVolume(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	volume, err := c.volumeRepo.Get(c.getApp(ctx).ID, params.ByName("volume_id"))
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, volume)
}

func (c *controllerAPI) PutVolume(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var volume ct.Volume
	if err := httphelper.DecodeJSON(req, &volume); err != nil {
		respondWithError(w, err)
		return
	}

	if err := schema.Validate(volume); err != nil {
		respondWithError(w, err)
		return
	}

	if err := c.volumeRepo.Add(&volume); err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &volume)
}

func (c *controllerAPI) DecommissionVolume(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var volume ct.Volume
	if err := httphelper.DecodeJSON(req, &volume); err != nil {
		respondWithError(w, err)
		return
	}
	if err := c.volumeRepo.Decommission(c.getApp(ctx).ID, &volume); err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &volume)
}

func (c *controllerAPI) streamVolumes(ctx context.Context, w http.ResponseWriter, req *http.Request) (err error) {
	l, _ := ctxhelper.LoggerFromContext(ctx)
	ch := make(chan *ct.Volume)
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

	sub, err := eventListener.Subscribe("", []string{string(ct.EventTypeVolume)}, "")
	if err != nil {
		return err
	}
	defer sub.Close()

	vols, err := c.volumeRepo.ListSince(since)
	if err != nil {
		return err
	}
	currentUpdatedAt := since
	for _, vol := range vols {
		select {
		case <-stream.Done:
			return nil
		case ch <- vol:
			if vol.UpdatedAt.After(currentUpdatedAt) {
				currentUpdatedAt = *vol.UpdatedAt
			}
		}
	}

	select {
	case <-stream.Done:
		return nil
	case ch <- &ct.Volume{}:
	}

	for {
		select {
		case <-stream.Done:
			return
		case event, ok := <-sub.Events:
			if !ok {
				return sub.Err
			}
			var vol ct.Volume
			if err := json.Unmarshal(event.Data, &vol); err != nil {
				l.Error("error deserializing volume event", "event.id", event.ID, "err", err)
				continue
			}
			if vol.UpdatedAt.Before(currentUpdatedAt) {
				continue
			}
			select {
			case <-stream.Done:
				return nil
			case ch <- &vol:
			}
		}
	}
}
