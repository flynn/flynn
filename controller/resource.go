package main

import (
	"net/http"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/resource"
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
		if err := createEvent(tx.Exec, &ct.Event{
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
		if err := createEvent(tx.Exec, &ct.Event{
			ObjectID:   r.ID,
			ObjectType: ct.EventTypeResource,
		}, r); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func split(s string, sep string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
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
		if err := createEvent(tx.Exec, &ct.Event{
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
		if err := createEvent(tx.Exec, &ct.Event{
			ObjectID:   r.ID,
			ObjectType: ct.EventTypeResourceDeletion,
		}, r); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (c *controllerAPI) ProvisionResource(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	p, err := c.getProvider(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	var rr ct.ResourceReq
	if err = httphelper.DecodeJSON(req, &rr); err != nil {
		respondWithError(w, err)
		return
	}

	var config []byte
	if rr.Config != nil {
		config = *rr.Config
	} else {
		config = []byte(`{}`)
	}
	data, err := resource.Provision(p.URL, config)
	if err != nil {
		respondWithError(w, err)
		return
	}

	res := &ct.Resource{
		ProviderID: p.ID,
		ExternalID: data.ID,
		Env:        data.Env,
		Apps:       rr.Apps,
	}

	if err := schema.Validate(res); err != nil {
		respondWithError(w, err)
		return
	}

	if err := c.resourceRepo.Add(res); err != nil {
		// TODO: attempt to "rollback" provisioning
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, res)
}

func (c *controllerAPI) GetProviderResources(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	p, err := c.getProvider(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	res, err := c.resourceRepo.ProviderList(p.ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, res)
}

func (c *controllerAPI) GetResources(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	res, err := c.resourceRepo.List()
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, res)
}

func (c *controllerAPI) GetResource(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	_, err := c.getProvider(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	res, err := c.resourceRepo.Get(params.ByName("resources_id"))
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, res)
}

func (c *controllerAPI) PutResource(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	p, err := c.getProvider(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	var resource ct.Resource
	if err = httphelper.DecodeJSON(req, &resource); err != nil {
		respondWithError(w, err)
		return
	}

	resource.ID = params.ByName("resources_id")
	resource.ProviderID = p.ID

	if err := schema.Validate(resource); err != nil {
		respondWithError(w, err)
		return
	}

	if err := c.resourceRepo.Add(&resource); err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &resource)
}

func (c *controllerAPI) DeleteResource(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	id := params.ByName("resources_id")

	logger.Info("getting provider", "params", params)

	p, err := c.getProvider(ctx)
	if err != nil {
		logger.Error("getting provider error", "err", err)
		respondWithError(w, err)
		return
	}

	logger.Info("getting resource", "id", id)
	res, err := c.resourceRepo.Get(id)
	if err != nil {
		logger.Error("getting resource error", "err", err)
		respondWithError(w, err)
		return
	}

	logger.Info("deprovisioning", "url", p.URL, "external.id", res.ExternalID)
	if err := resource.Deprovision(p.URL, res.ExternalID); err != nil {
		logger.Error("error deprovisioning", "err", err)
		respondWithError(w, err)
		return
	}

	logger.Info("removing resource")
	if err := c.resourceRepo.Remove(res); err != nil {
		logger.Error("error removing resource", "err", err)
		respondWithError(w, err)
		return
	}
	logger.Info("completed resource removal")

	httphelper.JSON(w, 200, res)
}

func (c *controllerAPI) GetAppResources(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	res, err := c.resourceRepo.AppList(c.getApp(ctx).ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, res)
}
