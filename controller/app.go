package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/flynn/flynn/controller/name"
	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	logaggc "github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/sse"
	routerc "github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
	"github.com/flynn/que-go"
	"github.com/jackc/pgx"
	"golang.org/x/net/context"
)

type AppRepo struct {
	router        routerc.Client
	defaultDomain string

	db *postgres.DB
}

type appUpdate map[string]interface{}

func NewAppRepo(db *postgres.DB, defaultDomain string, router routerc.Client) *AppRepo {
	return &AppRepo{db: db, defaultDomain: defaultDomain, router: router}
}

func (r *AppRepo) Add(data interface{}) error {
	app := data.(*ct.App)
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	if app.Name == "" {
		var nameID int64
		if err := tx.QueryRow("app_next_name_id").Scan(&nameID); err != nil {
			tx.Rollback()
			return err
		}
		// Safe cast because name_ids is limited to 32 bit size in schema
		app.Name = name.Get(uint32(nameID))
	}
	if len(app.Name) > 100 || !utils.AppNamePattern.MatchString(app.Name) {
		return ct.ValidationError{Field: "name", Message: "is invalid"}
	}
	if app.ID == "" {
		app.ID = random.UUID()
	}
	if app.Strategy == "" {
		app.Strategy = "all-at-once"
	}
	if app.DeployTimeout == 0 {
		app.DeployTimeout = ct.DefaultDeployTimeout
	}
	if err := tx.QueryRow("app_insert", app.ID, app.Name, app.Meta, app.Strategy, app.DeployTimeout).Scan(&app.CreatedAt, &app.UpdatedAt); err != nil {
		tx.Rollback()
		if postgres.IsUniquenessError(err, "apps_name_idx") {
			return httphelper.ObjectExistsErr(fmt.Sprintf("application %q already exists", app.Name))
		}
		return err
	}

	if app.Meta == nil {
		// ensure we don't return `{"meta": null}`
		app.Meta = make(map[string]string)
	}

	if err := createEvent(tx.Exec, &ct.Event{
		AppID:      app.ID,
		ObjectID:   app.ID,
		ObjectType: ct.EventTypeApp,
	}, app); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	if !app.System() && r.defaultDomain != "" {
		route := (&router.HTTPRoute{
			Domain:        fmt.Sprintf("%s.%s", app.Name, r.defaultDomain),
			Service:       app.Name + "-web",
			DrainBackends: true,
		}).ToRoute()
		if err := createRoute(r.db, r.router, app.ID, route); err != nil {
			log.Printf("Error creating default route for %s: %s", app.Name, err)
		}
	}
	return nil
}

func scanApp(s postgres.Scanner) (*ct.App, error) {
	app := &ct.App{}
	var releaseID *string
	err := s.Scan(&app.ID, &app.Name, &app.Meta, &app.Strategy, &releaseID, &app.DeployTimeout, &app.CreatedAt, &app.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if releaseID != nil {
		app.ReleaseID = *releaseID
	}
	if app.Meta == nil {
		// ensure `{}` rather than `null` when serializing to JSON
		app.Meta = map[string]string{}
	}
	return app, err
}

var idPattern = regexp.MustCompile(`^[a-f0-9]{8}-?([a-f0-9]{4}-?){3}[a-f0-9]{12}$`)

type rowQueryer interface {
	QueryRow(query string, args ...interface{}) postgres.Scanner
}

func selectApp(db rowQueryer, id string, update bool) (*ct.App, error) {
	var suffix string
	if update {
		suffix = "_for_update"
	}
	var row postgres.Scanner
	if idPattern.MatchString(id) {
		row = db.QueryRow("app_select_by_name_or_id"+suffix, id, id)
	} else {
		row = db.QueryRow("app_select_by_name"+suffix, id)
	}
	return scanApp(row)
}

func (r *AppRepo) Get(id string) (interface{}, error) {
	return selectApp(r.db, id, false)
}

func (r *AppRepo) Update(id string, data map[string]interface{}) (interface{}, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	app, err := selectApp(tx, id, true)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	for k, v := range data {
		switch k {
		case "strategy":
			strategy, ok := v.(string)
			if !ok {
				tx.Rollback()
				return nil, fmt.Errorf("controller: expected string, got %T", v)
			}
			app.Strategy = strategy
			if err := tx.Exec("app_update_strategy", app.ID, app.Strategy); err != nil {
				tx.Rollback()
				return nil, err
			}
		case "meta":
			data, ok := v.(map[string]interface{})
			if !ok {
				tx.Rollback()
				return nil, fmt.Errorf("controller: expected map[string]interface{}, got %T", v)
			}
			app.Meta = make(map[string]string, len(data))
			for k, v := range data {
				s, ok := v.(string)
				if !ok {
					tx.Rollback()
					return nil, fmt.Errorf("controller: expected string, got %T", v)
				}
				app.Meta[k] = s
			}
			if err := tx.Exec("app_update_meta", app.ID, app.Meta); err != nil {
				tx.Rollback()
				return nil, err
			}
		case "deploy_timeout":
			timeout, ok := v.(json.Number)
			if !ok {
				tx.Rollback()
				return nil, fmt.Errorf("controller: expected json.Number, got %T", v)
			}
			t, err := timeout.Int64()
			if err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("controller: unable to decode json.Number: %s", err)
			}
			app.DeployTimeout = int32(t)
			if err := tx.Exec("app_update_deploy_timeout", app.ID, app.DeployTimeout); err != nil {
				tx.Rollback()
				return nil, err
			}
		}
	}

	if err := createEvent(tx.Exec, &ct.Event{
		AppID:      app.ID,
		ObjectID:   app.ID,
		ObjectType: ct.EventTypeApp,
	}, app); err != nil {
		tx.Rollback()
		return nil, err
	}

	return app, tx.Commit()
}

func (r *AppRepo) List() (interface{}, error) {
	rows, err := r.db.Query("app_list")
	if err != nil {
		return nil, err
	}
	apps := []*ct.App{}
	for rows.Next() {
		app, err := scanApp(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		apps = append(apps, app)
	}
	return apps, rows.Err()
}

func (r *AppRepo) SetRelease(app *ct.App, releaseID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	var release *ct.Release
	var prevRelease *ct.Release
	if app.ReleaseID != "" {
		row := tx.QueryRow("release_select", app.ReleaseID)
		prevRelease, _ = scanRelease(row)
	}
	row := tx.QueryRow("release_select", releaseID)
	if release, err = scanRelease(row); err != nil {
		return err
	}
	app.ReleaseID = releaseID
	if err := tx.Exec("app_update_release", app.ID, app.ReleaseID); err != nil {
		tx.Rollback()
		return err
	}
	if err := createEvent(tx.Exec, &ct.Event{
		AppID:      app.ID,
		ObjectID:   release.ID,
		ObjectType: ct.EventTypeAppRelease,
	}, &ct.AppRelease{
		PrevRelease: prevRelease,
		Release:     release,
	}); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *AppRepo) GetRelease(id string) (*ct.Release, error) {
	row := r.db.QueryRow("app_get_release", id)
	return scanRelease(row)
}

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

	opts := logaggc.LogOpts{
		Follow: req.FormValue("follow") == "true",
		JobID:  req.FormValue("job_id"),
	}
	if vals, ok := req.Form["process_type"]; ok && len(vals) > 0 {
		opts.ProcessType = &vals[len(vals)-1]
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
