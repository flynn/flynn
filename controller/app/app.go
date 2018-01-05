package apprepo

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/flynn/flynn/controller/common"
	"github.com/flynn/flynn/controller/name"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	routerc "github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
	"github.com/jackc/pgx"
)

type Repo struct {
	router        routerc.Client
	defaultDomain string

	db *postgres.DB
}

func NewRepo(db *postgres.DB, defaultDomain string, router routerc.Client) *Repo {
	return &Repo{db: db, defaultDomain: defaultDomain, router: router}
}

func (r *Repo) Add(data interface{}) error {
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
	if app.Meta == nil {
		app.Meta = make(map[string]string)
	}
	if _, ok := app.Meta["gc.max_inactive_slug_releases"]; !ok {
		app.Meta["gc.max_inactive_slug_releases"] = "10"
	}

	if err := tx.QueryRow("app_insert", app.ID, app.Name, app.Meta, app.Strategy, app.DeployTimeout).Scan(&app.CreatedAt, &app.UpdatedAt); err != nil {
		tx.Rollback()
		if postgres.IsUniquenessError(err, "apps_name_idx") {
			return httphelper.ObjectExistsErr(fmt.Sprintf("application %q already exists", app.Name))
		}
		return err
	}

	if err := common.CreateEvent(tx.Exec, &ct.Event{
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
		if err := common.CreateRoute(r.db, r.router, app.ID, route); err != nil {
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
		return nil, common.ErrNotFound
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

type rowQueryer interface {
	QueryRow(query string, args ...interface{}) postgres.Scanner
}

func selectApp(db rowQueryer, id string, update bool) (*ct.App, error) {
	var suffix string
	if update {
		suffix = "_for_update"
	}
	var row postgres.Scanner
	if common.IDPattern.MatchString(id) {
		row = db.QueryRow("app_select_by_name_or_id"+suffix, id, id)
	} else {
		row = db.QueryRow("app_select_by_name"+suffix, id)
	}
	return scanApp(row)
}

func (r *Repo) Get(id string) (interface{}, error) {
	return selectApp(r.db, id, false)
}

func (r *Repo) Update(id string, data map[string]interface{}) (interface{}, error) {
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

	if err := common.CreateEvent(tx.Exec, &ct.Event{
		AppID:      app.ID,
		ObjectID:   app.ID,
		ObjectType: ct.EventTypeApp,
	}, app); err != nil {
		tx.Rollback()
		return nil, err
	}

	return app, tx.Commit()
}

func (r *Repo) List() (interface{}, error) {
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

func (r *Repo) SetRelease(app *ct.App, releaseID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	var release *ct.Release
	var prevRelease *ct.Release
	if app.ReleaseID != "" {
		row := tx.QueryRow("release_select", app.ReleaseID)
		prevRelease, _ = common.ScanRelease(row)
	}
	row := tx.QueryRow("release_select", releaseID)
	if release, err = common.ScanRelease(row); err != nil {
		return err
	}
	app.ReleaseID = releaseID
	if err := tx.Exec("app_update_release", app.ID, app.ReleaseID); err != nil {
		tx.Rollback()
		return err
	}
	if err := common.CreateEvent(tx.Exec, &ct.Event{
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

func (r *Repo) GetRelease(id string) (*ct.Release, error) {
	row := r.db.QueryRow("app_get_release", id)
	return common.ScanRelease(row)
}
