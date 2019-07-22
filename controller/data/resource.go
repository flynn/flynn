package data

import (
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/jackc/pgx"
)

type ResourceRepo struct {
	db *postgres.DB
}

func NewResourceRepo(db *postgres.DB) *ResourceRepo {
	return &ResourceRepo{db}
}

func (rr *ResourceRepo) Add(r *ct.Resource) error {
	if r.ID == "" {
		r.ID = random.UUID()
	}
	tx, err := rr.db.Begin()
	if err != nil {
		return err
	}
	err = tx.QueryRow("resource_insert", r.ID, r.ProviderID, r.ExternalID, r.Env).Scan(&r.CreatedAt)
	if err != nil {
		tx.Rollback()
		return err
	}
	for i, appID := range r.Apps {
		var row postgres.Scanner
		if idPattern.MatchString(appID) {
			row = tx.QueryRow("app_resource_insert_app_by_name_or_id", appID, appID, r.ID)
		} else {
			row = tx.QueryRow("app_resource_insert_app_by_name", appID, r.ID)
		}
		if err := row.Scan(&r.Apps[i]); err != nil {
			tx.Rollback()
			return err
		}
	}
	for _, appID := range r.Apps {
		if err := CreateEvent(tx.Exec, &ct.Event{
			AppID:      appID,
			ObjectID:   r.ID,
			ObjectType: ct.EventTypeResource,
		}, r); err != nil {
			tx.Rollback()
			return err
		}
	}
	if len(r.Apps) == 0 {
		// Ensure an event is created if there are no associated apps
		if err := CreateEvent(tx.Exec, &ct.Event{
			ObjectID:   r.ID,
			ObjectType: ct.EventTypeResource,
		}, r); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (rr *ResourceRepo) AddApp(resourceID, appID string) (*ct.Resource, error) {
	tx, err := rr.db.Begin()
	if err != nil {
		return nil, err
	}

	row := tx.QueryRow("resource_select", resourceID)
	r, err := scanResource(row)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	r.Apps = append(r.Apps, appID)

	{
		var row postgres.Scanner
		if idPattern.MatchString(appID) {
			row = tx.QueryRow("app_resource_insert_app_by_name_or_id", appID, appID, r.ID)
		} else {
			row = tx.QueryRow("app_resource_insert_app_by_name", appID, r.ID)
		}
		if err := row.Scan(&r.Apps[len(r.Apps)-1]); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	if err := CreateEvent(tx.Exec, &ct.Event{
		AppID:      appID,
		ObjectID:   r.ID,
		ObjectType: ct.EventTypeResource,
	}, r); err != nil {
		tx.Rollback()
		return nil, err
	}
	return r, tx.Commit()
}

func (rr *ResourceRepo) RemoveApp(resourceID, appID string) (*ct.Resource, error) {
	tx, err := rr.db.Begin()
	if err != nil {
		return nil, err
	}

	row := tx.QueryRow("resource_select", resourceID)
	r, err := scanResource(row)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	apps := make([]string, 0, len(r.Apps))
	for _, id := range r.Apps {
		if id != appID {
			apps = append(apps, id)
		}
	}
	r.Apps = apps

	if err := tx.Exec("app_resource_delete_by_app", appID); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := CreateEvent(tx.Exec, &ct.Event{
		AppID:      appID,
		ObjectID:   r.ID,
		ObjectType: ct.EventTypeResourceAppDeletion,
	}, r); err != nil {
		tx.Rollback()
		return nil, err
	}
	return r, tx.Commit()
}

func scanResource(s postgres.Scanner) (*ct.Resource, error) {
	r := &ct.Resource{}
	var appIDs string
	err := s.Scan(&r.ID, &r.ProviderID, &r.ExternalID, &r.Env, &appIDs, &r.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if appIDs != "" {
		r.Apps = split(appIDs[1:len(appIDs)-1], ",")
	}
	return r, err
}

func (r *ResourceRepo) Get(id string) (*ct.Resource, error) {
	row := r.db.QueryRow("resource_select", id)
	return scanResource(row)
}

func (r *ResourceRepo) List() ([]*ct.Resource, error) {
	rows, err := r.db.Query("resource_list")
	if err != nil {
		return nil, err
	}
	return resourceList(rows)
}

func (r *ResourceRepo) ProviderList(providerID string) ([]*ct.Resource, error) {
	rows, err := r.db.Query("resource_list_by_provider", providerID)
	if err != nil {
		return nil, err
	}
	return resourceList(rows)
}

func resourceList(rows *pgx.Rows) ([]*ct.Resource, error) {
	var resources []*ct.Resource
	for rows.Next() {
		resource, err := scanResource(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		resources = append(resources, resource)
	}
	return resources, rows.Err()
}

func (r *ResourceRepo) AppList(appID string) ([]*ct.Resource, error) {
	rows, err := r.db.Query("resource_list_by_app", appID)
	if err != nil {
		return nil, err
	}
	return resourceList(rows)
}

func (rr *ResourceRepo) Remove(r *ct.Resource) error {
	tx, err := rr.db.Begin()
	if err != nil {
		return err
	}
	err = tx.Exec("resource_delete", r.ID)
	if err != nil {
		tx.Rollback()
		return err
	}
	err = tx.Exec("app_resource_delete_by_resource", r.ID)
	if err != nil {
		tx.Rollback()
		return err
	}
	for _, appID := range r.Apps {
		if err := CreateEvent(tx.Exec, &ct.Event{
			AppID:      appID,
			ObjectID:   r.ID,
			ObjectType: ct.EventTypeResourceDeletion,
		}, r); err != nil {
			tx.Rollback()
			return err
		}
	}
	if len(r.Apps) == 0 {
		// Ensure an event is created if there are no associated apps
		if err := CreateEvent(tx.Exec, &ct.Event{
			ObjectID:   r.ID,
			ObjectType: ct.EventTypeResourceDeletion,
		}, r); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
