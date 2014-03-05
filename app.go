package main

import (
	"database/sql"
	"errors"
	"regexp"

	ct "github.com/flynn/flynn-controller/types"
)

type AppRepo struct {
	db *DB
}

func NewAppRepo(db *DB) *AppRepo {
	return &AppRepo{db}
}

var appNamePattern = regexp.MustCompile(`^[a-z\d]+(-[a-z\d]+)*$`)

func (r *AppRepo) Add(data interface{}) error {
	app := data.(*ct.App)
	// TODO: actually validate
	if app.Name == "" {
		return errors.New("controller: app name must not be blank")
	}
	if len(app.Name) > 30 || !appNamePattern.MatchString(app.Name) {
		return errors.New("controller: invalid app name")
	}
	err := r.db.QueryRow("INSERT INTO apps (name) VALUES ($1) RETURNING app_id, created_at, updated_at", app.Name).Scan(&app.ID, &app.CreatedAt, &app.UpdatedAt)
	app.ID = cleanUUID(app.ID)
	return err
}

var ErrNotFound = errors.New("controller: resource not found")

func scanApp(s Scanner) (*ct.App, error) {
	app := &ct.App{}
	err := s.Scan(&app.ID, &app.Name, &app.CreatedAt, &app.UpdatedAt)
	if err == sql.ErrNoRows {
		err = ErrNotFound
	}
	app.ID = cleanUUID(app.ID)
	return app, err
}

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (r *AppRepo) Get(id string) (interface{}, error) {
	var rows Scanner
	query := "SELECT app_id, name, created_at, updated_at FROM apps WHERE deleted_at IS NULL AND "
	if idPattern.MatchString(id) {
		rows = r.db.QueryRow(query+"(app_id = $1 OR name = $2)", id, id)
	} else {
		rows = r.db.QueryRow(query+"name = $1", id)
	}
	return scanApp(rows)
}

func (r *AppRepo) List() (interface{}, error) {
	rows, err := r.db.Query("SELECT app_id, name, created_at, updated_at FROM apps WHERE deleted_at IS NULL ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	var apps []*ct.App
	for rows.Next() {
		app, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	return apps, rows.Err()
}

func (r *AppRepo) SetRelease(appID string, releaseID string) error {
	return r.db.Exec("UPDATE apps SET release_id = $2, updated_at = current_timestamp WHERE app_id = $1", appID, releaseID)
}

func (r *AppRepo) GetRelease(id string) (*ct.Release, error) {
	row := r.db.QueryRow("SELECT r.release_id, r.artifact_id, r.data, r.created_at FROM apps a JOIN releases r USING (release_id) WHERE a.app_id = $1", id)
	return scanRelease(row)
}
