package main

import (
	"net/http"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq/hstore"
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
	err = tx.QueryRow(`INSERT INTO resources (resource_id, provider_id, external_id, env)
					   VALUES ($1, $2, $3, $4)
					   RETURNING created_at`,
		r.ID, r.ProviderID, r.ExternalID, envHstore(r.Env)).Scan(&r.CreatedAt)
	if err != nil {
		tx.Rollback()
		return err
	}
	for i, appID := range r.Apps {
		var filterSQL string
		var args []interface{}
		if idPattern.MatchString(appID) {
			filterSQL = "app_id = $1 OR name = $2), $3)"
			args = []interface{}{appID, appID, r.ID}
		} else {
			filterSQL = "name = $1), $2)"
			args = []interface{}{appID, r.ID}
		}
		err = tx.QueryRow("INSERT INTO app_resources (app_id, resource_id) VALUES ((SELECT app_id FROM apps WHERE "+
			filterSQL+" RETURNING app_id", args...).Scan(&r.Apps[i])
		if err != nil {
			tx.Rollback()
			return err
		}
		r.Apps[i] = postgres.CleanUUID(r.Apps[i])
	}
	r.ID = postgres.CleanUUID(r.ID)
	return tx.Commit()
}

func envHstore(m map[string]string) hstore.Hstore {
	res := hstore.Hstore{Map: make(map[string]sql.NullString, len(m))}
	for k, v := range m {
		res.Map[k] = sql.NullString{String: v, Valid: true}
	}
	return res
}

func split(s string, sep string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func scanResource(s postgres.Scanner) (*ct.Resource, error) {
	r := &ct.Resource{}
	var env hstore.Hstore
	var appIDs string
	err := s.Scan(&r.ID, &r.ProviderID, &r.ExternalID, &env, &appIDs, &r.CreatedAt)
	if err == sql.ErrNoRows {
		err = ErrNotFound
	}
	r.ID = postgres.CleanUUID(r.ID)
	r.ProviderID = postgres.CleanUUID(r.ProviderID)
	r.Env = make(map[string]string, len(env.Map))
	for k, v := range env.Map {
		r.Env[k] = v.String
	}
	if appIDs != "" {
		r.Apps = split(appIDs[1:len(appIDs)-1], ",")
	}
	for i, id := range r.Apps {
		r.Apps[i] = postgres.CleanUUID(id)
	}
	return r, err
}

func (r *ResourceRepo) Get(id string) (*ct.Resource, error) {
	row := r.db.QueryRow(`SELECT resource_id, provider_id, external_id, env,
								 ARRAY(SELECT app_id
								       FROM app_resources a
									   WHERE a.resource_id = r.resource_id AND a.deleted_at IS NULL
									   ORDER BY a.created_at DESC),
								 created_at
						  FROM resources r
						  WHERE resource_id = $1 AND deleted_at IS NULL`, id)
	return scanResource(row)
}

func (r *ResourceRepo) ProviderList(providerID string) ([]*ct.Resource, error) {
	rows, err := r.db.Query(`SELECT resource_id, provider_id, external_id, env,
									ARRAY(SELECT a.app_id
								          FROM app_resources a
                                          WHERE a.resource_id = r.resource_id AND a.deleted_at IS NULL
                                          ORDER BY a.created_at DESC),
									created_at
							 FROM resources r
							 WHERE provider_id = $1 AND deleted_at IS NULL
							 ORDER BY created_at DESC`, providerID)
	if err != nil {
		return nil, err
	}
	return resourceList(rows)
}

func resourceList(rows *sql.Rows) ([]*ct.Resource, error) {
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
	rows, err := r.db.Query(`SELECT DISTINCT(r.resource_id), r.provider_id, r.external_id, r.env,
									ARRAY(SELECT a.app_id
									      FROM app_resources a
										  WHERE a.resource_id = r.resource_id AND a.deleted_at IS NULL
										  ORDER BY a.created_at DESC),
									r.created_at
							 FROM resources r
							 JOIN app_resources a USING (resource_id)
							 WHERE a.app_id = $1 AND r.deleted_at IS NULL
							 ORDER BY r.created_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	return resourceList(rows)
}

func (r *ResourceRepo) Remove(id string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE resources SET deleted_at = now() WHERE resource_id = $1 AND deleted_at IS NULL", id)
	if err != nil {
		tx.Rollback()
		return err
	}
	_, err = tx.Exec("UPDATE app_resources SET deleted_at = now() WHERE resource_id = $1 AND deleted_at IS NULL", id)
	if err != nil {
		tx.Rollback()
		return err
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

	p, err := c.getProvider(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	res, err := c.resourceRepo.Get(id)
	if err != nil {
		respondWithError(w, err)
		return
	}

	if err := resource.Deprovision(p.URL, res.ExternalID); err != nil {
		respondWithError(w, err)
		return
	}

	if err := c.resourceRepo.Remove(id); err != nil {
		respondWithError(w, err)
		return
	}

	w.WriteHeader(200)
}

func (c *controllerAPI) GetAppResources(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	res, err := c.resourceRepo.AppList(c.getApp(ctx).ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, res)
}
