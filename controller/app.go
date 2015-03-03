package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq/hstore"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/controller/name"
	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	routerc "github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
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

var appNamePattern = regexp.MustCompile(`^[a-z\d]+(-[a-z\d]+)*$`)

func (r *AppRepo) Add(data interface{}) error {
	app := data.(*ct.App)
	if app.Name == "" {
		var nameID uint32
		if err := r.db.QueryRow("SELECT nextval('name_ids')").Scan(&nameID); err != nil {
			return err
		}
		app.Name = name.Get(nameID)
	}
	if len(app.Name) > 100 || !appNamePattern.MatchString(app.Name) {
		return ct.ValidationError{Field: "name", Message: "is invalid"}
	}
	if app.ID == "" {
		app.ID = random.UUID()
	}
	if app.Strategy == "" {
		app.Strategy = "all-at-once"
	}
	meta := metaToHstore(app.Meta)
	if err := r.db.QueryRow("INSERT INTO apps (app_id, name, meta, strategy) VALUES ($1, $2, $3, $4) RETURNING created_at, updated_at", app.ID, app.Name, meta, app.Strategy).Scan(&app.CreatedAt, &app.UpdatedAt); err != nil {
		return err
	}
	app.ID = postgres.CleanUUID(app.ID)
	if !app.System() && r.defaultDomain != "" {
		route := (&router.HTTPRoute{
			Domain:  fmt.Sprintf("%s.%s", app.Name, r.defaultDomain),
			Service: app.Name + "-web",
		}).ToRoute()
		route.ParentRef = routeParentRef(app.ID)
		if err := r.router.CreateRoute(route); err != nil {
			log.Printf("Error creating default route for %s: %s", app.Name, err)
		}
	}
	return nil
}

func scanApp(s postgres.Scanner) (*ct.App, error) {
	app := &ct.App{}
	var meta hstore.Hstore
	err := s.Scan(&app.ID, &app.Name, &meta, &app.Strategy, &app.CreatedAt, &app.UpdatedAt)
	if err == sql.ErrNoRows {
		err = ErrNotFound
	}
	if len(meta.Map) > 0 {
		app.Meta = make(map[string]string, len(meta.Map))
		for k, v := range meta.Map {
			app.Meta[k] = v.String
		}
	}
	app.ID = postgres.CleanUUID(app.ID)
	return app, err
}

var idPattern = regexp.MustCompile(`^[a-f0-9]{8}-?([a-f0-9]{4}-?){3}[a-f0-9]{12}$`)

type rowQueryer interface {
	QueryRow(query string, args ...interface{}) postgres.Scanner
}

func selectApp(db rowQueryer, id string, update bool) (*ct.App, error) {
	var row postgres.Scanner
	query := "SELECT app_id, name, meta, strategy, created_at, updated_at FROM apps WHERE deleted_at IS NULL AND "
	var suffix string
	if update {
		suffix = " FOR UPDATE"
	}
	if idPattern.MatchString(id) {
		row = db.QueryRow(query+"(app_id = $1 OR name = $2) LIMIT 1"+suffix, id, id)
	} else {
		row = db.QueryRow(query+"name = $1"+suffix, id)
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
			if _, err := tx.Exec("UPDATE apps SET strategy = $2, updated_at = now() WHERE app_id = $1", app.ID, strategy); err != nil {
				tx.Rollback()
				return nil, err
			}
		case "meta":
			data, ok := v.(map[string]interface{})
			if !ok {
				tx.Rollback()
				return nil, fmt.Errorf("controller: expected map[string]interface{}, got %T", v)
			}
			var meta hstore.Hstore
			meta.Map = make(map[string]sql.NullString, len(data))
			app.Meta = make(map[string]string, len(data))
			for k, v := range data {
				s, ok := v.(string)
				if !ok {
					tx.Rollback()
					return nil, fmt.Errorf("controller: expected string, got %T", v)
				}
				meta.Map[k] = sql.NullString{String: s, Valid: true}
				app.Meta[k] = s
			}
			if _, err := tx.Exec("UPDATE apps SET meta = $2, updated_at = now() WHERE app_id = $1", app.ID, meta); err != nil {
				tx.Rollback()
				return nil, err
			}
		}
	}

	return app, tx.Commit()
}

func (r *AppRepo) Remove(id string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	if !idPattern.MatchString(id) {
		app, err := selectApp(r.db, id, false)
		if err != nil {
			return err
		}
		id = app.ID
	}
	_, err = tx.Exec("UPDATE apps SET deleted_at = now() WHERE app_id = $1 AND deleted_at IS NULL", id)
	if err != nil {
		tx.Rollback()
		return err
	}

	_, err = tx.Exec("UPDATE formations SET deleted_at = now(), processes = NULL, updated_at = now() WHERE app_id = $1 AND deleted_at IS NULL", id)
	if err != nil {
		tx.Rollback()
		return err
	}
	_, err = tx.Exec("UPDATE app_resources SET deleted_at = now() WHERE app_id = $1 AND deleted_at IS NULL", id)
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *AppRepo) List() (interface{}, error) {
	rows, err := r.db.Query("SELECT app_id, name, meta, strategy, created_at, updated_at FROM apps WHERE deleted_at IS NULL ORDER BY created_at DESC")
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

func (r *AppRepo) SetRelease(appID string, releaseID string) error {
	return r.db.Exec("UPDATE apps SET release_id = $2, updated_at = now() WHERE app_id = $1", appID, releaseID)
}

func (r *AppRepo) GetRelease(id string) (*ct.Release, error) {
	row := r.db.QueryRow("SELECT r.release_id, r.artifact_id, r.data, r.created_at FROM apps a JOIN releases r USING (release_id) WHERE a.app_id = $1", id)
	return scanRelease(row)
}

func (c *controllerAPI) UpdateApp(ctx context.Context, rw http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	var data appUpdate
	if err := httphelper.DecodeJSON(req, &data); err != nil {
		respondWithError(rw, err)
		return
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

func (c *controllerAPI) AppLog(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithCancel(ctx)
	params, _ := ctxhelper.ParamsFromContext(ctx)

	lines, _ := strconv.Atoi(req.FormValue("lines"))
	follow := req.FormValue("follow") == "true"
	// TODO(bgentry): support filtering by fields like process type/ID.

	rc, err := c.logaggc.GetLog(params.ByName("apps_id"), lines, follow)
	if err != nil {
		respondWithError(w, err)
		return
	}

	if cn, ok := w.(http.CloseNotifier); ok {
		go func() {
			select {
			case <-cn.CloseNotify():
				rc.Close()
			case <-ctx.Done():
			}
		}()
	}
	defer cancel()
	defer rc.Close()

	// TODO(bgentry): use SSE, distinguish between streams like JobLog.
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(200)
	// Send headers right away if following
	if wf, ok := w.(http.Flusher); ok && follow {
		wf.Flush()
	}

	fw := httphelper.FlushWriter{Writer: w, Enabled: follow}
	io.Copy(fw, rc)
}
