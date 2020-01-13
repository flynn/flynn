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

func (c *controllerAPI) PutFormation(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	app := c.getApp(ctx)
	release, err := c.getRelease(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	formation := &ct.Formation{}
	if err = httphelper.DecodeJSON(r, formation); err != nil {
		respondWithError(w, err)
		return
	}

	if len(release.ArtifactIDs) == 0 {
		respondWithError(w, ct.ValidationError{Message: "release is not deployable"})
		return
	}

	formation.AppID = app.ID
	formation.ReleaseID = release.ID

	if err = schema.Validate(formation); err != nil {
		respondWithError(w, err)
		return
	}

	req := newScaleRequest(formation, release)
	req, err = c.formationRepo.AddScaleRequest(req, false)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, scaleRequestAsFormation(req))
}

func (c *controllerAPI) PutScaleRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	app := c.getApp(ctx)
	release, err := c.getRelease(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	var req ct.ScaleRequest
	if err := httphelper.DecodeJSON(r, &req); err != nil {
		respondWithError(w, err)
		return
	}
	req.AppID = app.ID
	req.ReleaseID = release.ID

	if err := schema.Validate(req); err != nil {
		respondWithError(w, err)
		return
	}

	if req.State == ct.ScaleRequestStatePending {
		_, err = c.formationRepo.AddScaleRequest(&req, false)
	} else {
		err = c.formationRepo.UpdateScaleRequest(&req)
	}
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &req)
}

func (c *controllerAPI) GetFormation(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	app := c.getApp(ctx)
	releaseID := params.ByName("releases_id")
	if req.URL.Query().Get("expand") == "true" {
		formation, err := c.formationRepo.GetExpanded(app.ID, releaseID, false)
		if err != nil {
			respondWithError(w, err)
			return
		}
		httphelper.JSON(w, 200, formation)
		return
	}

	formation, err := c.formationRepo.Get(app.ID, releaseID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, formation)
}

func (c *controllerAPI) DeleteFormation(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	app := c.getApp(ctx)
	release, err := c.getRelease(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}
	formation, err := c.formationRepo.Get(app.ID, release.ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	formation.Processes = nil
	req := newScaleRequest(formation, release)
	if _, err := c.formationRepo.AddScaleRequest(req, true); err != nil {
		respondWithError(w, err)
		return
	}
	w.WriteHeader(200)
}

func (c *controllerAPI) ListFormations(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	app := c.getApp(ctx)
	list, err := c.formationRepo.List(app.ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, list)
}

func (c *controllerAPI) GetFormations(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		c.streamFormations(ctx, w, req)
		return
	}

	if req.URL.Query().Get("active") == "true" {
		list, err := c.formationRepo.ListActive()
		if err != nil {
			respondWithError(w, err)
			return
		}
		httphelper.JSON(w, 200, list)
		return
	}

	// don't return a list of all formations, there will be lots of them
	// and no components currently need such a list
	httphelper.ValidationError(w, "", "must either request a stream or only active formations")
}

func (c *controllerAPI) streamFormations(ctx context.Context, w http.ResponseWriter, req *http.Request) (err error) {
	l, _ := ctxhelper.LoggerFromContext(ctx)
	ch := make(chan *ct.ExpandedFormation)
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
		l.Error("error starting event listener", "err", err)
		return err
	}

	sub, err := eventListener.Subscribe(nil, []string{string(ct.EventTypeScaleRequest)}, nil)
	if err != nil {
		return err
	}
	defer sub.Close()

	formations, err := c.formationRepo.ListSince(since)
	if err != nil {
		return err
	}
	currentUpdatedAt := since
	for _, formation := range formations {
		select {
		case <-stream.Done:
			return nil
		case ch <- formation:
			if formation.UpdatedAt.After(currentUpdatedAt) {
				currentUpdatedAt = formation.UpdatedAt
			}
		}
	}

	select {
	case <-stream.Done:
		return nil
	case ch <- &ct.ExpandedFormation{}:
	}

	for {
		select {
		case <-stream.Done:
			return
		case event, ok := <-sub.Events:
			if !ok {
				return sub.Err
			}
			var req ct.ScaleRequest
			if err := json.Unmarshal(event.Data, &req); err != nil {
				l.Error("error deserializing scale event", "event.id", event.ID, "err", err)
				continue
			}
			formation, err := c.formationRepo.GetExpanded(req.AppID, req.ReleaseID, true)
			if err != nil {
				l.Error("error expanding formation", "app.id", req.AppID, "release.id", req.ReleaseID, "err", err)
				continue
			}
			if formation.UpdatedAt.Before(currentUpdatedAt) {
				continue
			}
			select {
			case <-stream.Done:
				return nil
			case ch <- formation:
			}
		}
	}
}

func newScaleRequest(f *ct.Formation, release *ct.Release) *ct.ScaleRequest {
	// treat nil processes as a request to scale everything down
	if f.Processes == nil {
		f.Processes = make(map[string]int, len(release.Processes))
		for typ := range release.Processes {
			f.Processes[typ] = 0
		}
	}
	return &ct.ScaleRequest{
		AppID:        f.AppID,
		ReleaseID:    f.ReleaseID,
		NewProcesses: &f.Processes,
		NewTags:      &f.Tags,
	}
}

func scaleRequestAsFormation(sr *ct.ScaleRequest) *ct.Formation {
	var processes map[string]int
	if sr.NewProcesses != nil {
		processes = *sr.NewProcesses
	}
	var tags map[string]map[string]string
	if sr.NewTags != nil {
		tags = *sr.NewTags
	}
	return &ct.Formation{
		AppID:     sr.AppID,
		ReleaseID: sr.ReleaseID,
		Processes: processes,
		Tags:      tags,
		CreatedAt: sr.CreatedAt,
		UpdatedAt: sr.UpdatedAt,
	}
}
