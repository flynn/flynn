package main

import (
	"net/http"

	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/resource"
	"golang.org/x/net/context"
)

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

func (c *controllerAPI) AddResourceApp(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	_, err := c.getProvider(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	resource, err := c.resourceRepo.AddApp(params.ByName("resources_id"), params.ByName("app_id"))
	if err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, resource)
}

func (c *controllerAPI) DeleteResourceApp(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	_, err := c.getProvider(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	resource, err := c.resourceRepo.RemoveApp(params.ByName("resources_id"), params.ByName("app_id"))
	if err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, resource)
}

func (c *controllerAPI) GetAppResources(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	res, err := c.resourceRepo.AppList(c.getApp(ctx).ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, res)
}
