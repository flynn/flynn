package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/flynn/flynn/controller/data"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/sse"
	"golang.org/x/net/context"
)

func (c *controllerAPI) maybeStartEventListener() (*data.EventListener, error) {
	c.eventListenerMtx.Lock()
	defer c.eventListenerMtx.Unlock()
	if c.eventListener != nil && !c.eventListener.IsClosed() {
		return c.eventListener, nil
	}
	c.eventListener = data.NewEventListener(c.eventRepo)
	return c.eventListener, c.eventListener.Listen()
}

func (c *controllerAPI) GetEvent(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	id, err := strconv.ParseInt(params.ByName("id"), 10, 64)
	if err != nil {
		respondWithError(w, err)
		return
	}
	event, err := c.eventRepo.GetEvent(id)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, event)
}

func (c *controllerAPI) Events(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	l, _ := ctxhelper.LoggerFromContext(ctx)
	log := l.New("fn", "Events")
	var app *ct.App
	if appID := req.FormValue("app_id"); appID != "" {
		data, err := c.appRepo.Get(appID)
		if err != nil {
			respondWithError(w, err)
			return
		}
		app = data.(*ct.App)
	}

	if req.Header.Get("Accept") == "application/json" {
		if err := listEvents(ctx, w, req, app, c.eventRepo); err != nil {
			log.Error("error listing events", "err", err)
			respondWithError(w, err)
		}
		return
	}

	eventListener, err := c.maybeStartEventListener()
	if err != nil {
		log.Error("error starting event listener", "err", err)
		respondWithError(w, err)
		return
	}
	if err := streamEvents(ctx, w, req, eventListener, app, c.eventRepo); err != nil {
		log.Error("error streaming events", "err", err)
		respondWithError(w, err)
	}
}

func listEvents(ctx context.Context, w http.ResponseWriter, req *http.Request, app *ct.App, repo *data.EventRepo) (err error) {
	var appID string
	if app != nil {
		appID = app.ID
	}

	var beforeID *int64
	if req.FormValue("before_id") != "" {
		id, err := strconv.ParseInt(req.FormValue("before_id"), 10, 64)
		if err != nil {
			return ct.ValidationError{Field: "before_id", Message: "is invalid"}
		}
		beforeID = &id
	}

	var sinceID *int64
	if req.FormValue("since_id") != "" {
		id, err := strconv.ParseInt(req.FormValue("since_id"), 10, 64)
		if err != nil {
			return ct.ValidationError{Field: "since_id", Message: "is invalid"}
		}
		sinceID = &id
	}

	var count int
	if req.FormValue("count") != "" {
		count, err = strconv.Atoi(req.FormValue("count"))
		if err != nil {
			return ct.ValidationError{Field: "count", Message: "is invalid"}
		}
	}

	objectTypes := strings.Split(req.FormValue("object_types"), ",")
	if len(objectTypes) == 1 && objectTypes[0] == "" {
		objectTypes = []string{}
	}
	objectID := req.FormValue("object_id")

	list, err := repo.ListEvents(appID, objectTypes, objectID, beforeID, sinceID, count)
	if err != nil {
		return err
	}
	httphelper.JSON(w, 200, list)
	return nil
}

func streamEvents(ctx context.Context, w http.ResponseWriter, req *http.Request, eventListener *data.EventListener, app *ct.App, repo *data.EventRepo) (err error) {
	var appID string
	if app != nil {
		appID = app.ID
	}

	var lastID int64
	if req.Header.Get("Last-Event-Id") != "" {
		lastID, err = strconv.ParseInt(req.Header.Get("Last-Event-Id"), 10, 64)
		if err != nil {
			return ct.ValidationError{Field: "Last-Event-Id", Message: "is invalid"}
		}
	}

	var count int
	if req.FormValue("count") != "" {
		count, err = strconv.Atoi(req.FormValue("count"))
		if err != nil {
			return ct.ValidationError{Field: "count", Message: "is invalid"}
		}
	}

	objectTypes := strings.Split(req.FormValue("object_types"), ",")
	if len(objectTypes) == 1 && objectTypes[0] == "" {
		objectTypes = []string{}
	}
	objectID := req.FormValue("object_id")
	past := req.FormValue("past")

	l, _ := ctxhelper.LoggerFromContext(ctx)
	log := l.New("fn", "streamEvents", "object_types", objectTypes, "object_id", objectID)
	ch := make(chan *ct.Event)
	s := sse.NewStream(w, ch, log)
	s.Serve()
	defer func() {
		if err == nil {
			s.Close()
		} else {
			s.CloseWithError(err)
		}
	}()

	sub, err := eventListener.Subscribe(appID, objectTypes, objectID)
	if err != nil {
		return err
	}
	defer sub.Close()

	var currID int64
	if past == "true" || lastID > 0 {
		list, err := repo.ListEvents(appID, objectTypes, objectID, nil, &lastID, count)
		if err != nil {
			return err
		}
		// events are in ID DESC order, so iterate in reverse
		for i := len(list) - 1; i >= 0; i-- {
			e := list[i]
			ch <- e
			currID = e.ID
		}
	}

	for {
		select {
		case <-s.Done:
			return
		case event, ok := <-sub.Events:
			if !ok {
				return sub.Err
			}
			if event.ID <= currID {
				continue
			}
			ch <- event
		}
	}
}
