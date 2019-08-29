package data

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"

	"github.com/flynn/flynn/controller/name"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	router "github.com/flynn/flynn/router/types"
	"github.com/jackc/pgx"
)

type AppRepo struct {
	routes        *RouteRepo
	defaultDomain string

	db *postgres.DB
}

func NewAppRepo(db *postgres.DB, defaultDomain string, routes *RouteRepo) *AppRepo {
	return &AppRepo{db: db, defaultDomain: defaultDomain, routes: routes}
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

	if err := CreateEvent(tx.Exec, &ct.Event{
		AppID:      app.ID,
		ObjectID:   app.ID,
		ObjectType: ct.EventTypeApp,
		Op:         ct.EventOpCreate,
	}, app); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	if !app.System() && r.defaultDomain != "" {
		route := (&router.HTTPRoute{
			ParentRef:     ct.RouteParentRefPrefix + app.ID,
			Domain:        fmt.Sprintf("%s.%s", app.Name, r.defaultDomain),
			Service:       app.Name + "-web",
			DrainBackends: true,
		}).ToRoute()
		if err := r.routes.Add(route); err != nil {
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
	return r.TxGet(r.db, id)
}

func (r *AppRepo) TxGet(tx rowQueryer, id string) (*ct.App, error) {
	return selectApp(tx, id, false)
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
			data, ok := v.(map[string]string)
			if !ok {
				datai, ok := v.(map[string]interface{})
				if !ok {
					tx.Rollback()
					return nil, fmt.Errorf("controller: expected map[string]interface{}, got %T", v)
				}
				data = make(map[string]string, len(datai))
				for k, v := range datai {
					s, ok := v.(string)
					if !ok {
						tx.Rollback()
						return nil, fmt.Errorf("controller: expected string, got %T", v)
					}
					data[k] = s
				}
			}
			app.Meta = data
			if err := tx.Exec("app_update_meta", app.ID, app.Meta); err != nil {
				tx.Rollback()
				return nil, err
			}
		case "deploy_timeout":
			timeout, ok := v.(int32)
			if !ok {
				t, ok := v.(json.Number)
				if !ok {
					tx.Rollback()
					return nil, fmt.Errorf("controller: expected json.Number, got %T", v)
				}
				timeout64, err := t.Int64()
				if err != nil {
					tx.Rollback()
					return nil, fmt.Errorf("controller: unable to decode json.Number: %s", err)
				}
				timeout = int32(timeout64)
			}
			app.DeployTimeout = timeout
			if err := tx.Exec("app_update_deploy_timeout", app.ID, app.DeployTimeout); err != nil {
				tx.Rollback()
				return nil, err
			}
		}
	}

	if err := CreateEvent(tx.Exec, &ct.Event{
		AppID:      app.ID,
		ObjectID:   app.ID,
		ObjectType: ct.EventTypeApp,
		Op:         ct.EventOpUpdate,
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

type ListAppOptions struct {
	PageToken    PageToken
	AppIDs       []string
	LabelFilters []ct.LabelFilter
}

func (r *AppRepo) ListPage(opts ListAppOptions) ([]*ct.App, *PageToken, error) {
	var pageSize int
	if opts.PageToken.Size > 0 {
		pageSize = opts.PageToken.Size
	} else {
		pageSize = DEFAULT_PAGE_SIZE
	}
	rows, err := r.db.Query("app_list_page", opts.PageToken.BeforeID, opts.AppIDs, opts.LabelFilters, pageSize+1)
	if err != nil {
		return nil, nil, err
	}
	apps := []*ct.App{}
	for rows.Next() {
		app, err := scanApp(rows)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		apps = append(apps, app)
	}

	var lastApp *ct.App
	var nextPageToken *PageToken
	if len(apps) == pageSize+1 {
		// remove the extra app from the list
		apps = apps[0:pageSize]
		lastApp = apps[0]
	}
	if lastApp != nil {
		nextPageToken = &PageToken{
			BeforeID: &lastApp.ID,
			Size:     pageSize,
		}
	}

	return apps, nextPageToken, rows.Err()
}

func (r *AppRepo) SetRelease(app *ct.App, releaseID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	if err := r.TxSetRelease(tx, app, releaseID); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *AppRepo) TxSetRelease(tx *postgres.DBTx, app *ct.App, releaseID string) error {
	var release *ct.Release
	var prevRelease *ct.Release
	if app.ReleaseID != "" {
		row := tx.QueryRow("release_select", app.ReleaseID)
		prevRelease, _ = scanRelease(row)
	}
	row := tx.QueryRow("release_select", releaseID)
	var err error
	if release, err = scanRelease(row); err != nil {
		return err
	}
	app.ReleaseID = releaseID
	if err := tx.QueryRow("app_update_release", app.ID, app.ReleaseID).Scan(&app.UpdatedAt); err != nil {
		return err
	}
	return CreateEvent(tx.Exec, &ct.Event{
		AppID:      app.ID,
		ObjectID:   release.ID,
		ObjectType: ct.EventTypeAppRelease,
	}, &ct.AppRelease{
		PrevRelease: prevRelease,
		Release:     release,
	})
}

func (r *AppRepo) GetRelease(id string) (*ct.Release, error) {
	return r.TxGetRelease(r.db, id)
}

func (r *AppRepo) TxGetRelease(tx rowQueryer, id string) (*ct.Release, error) {
	row := tx.QueryRow("app_get_release", id)
	return scanRelease(row)
}
