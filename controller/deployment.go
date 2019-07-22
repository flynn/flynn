package main

import (
	"net/http"

	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"golang.org/x/net/context"
)

func (c *controllerAPI) GetDeployment(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	deployment, err := c.deploymentRepo.Get(params.ByName("deployment_id"))
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, deployment)
}

func (c *controllerAPI) CreateDeployment(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var rid releaseID
	if err := httphelper.DecodeJSON(req, &rid); err != nil {
		respondWithError(w, err)
		return
	}
	appID := c.getApp(ctx).ID

	d, err := c.deploymentRepo.Add(appID, rid.ID)
	if err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, d)
}

func (c *controllerAPI) ListDeployments(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	app := c.getApp(ctx)
	list, err := c.deploymentRepo.List(app.ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, list)
}
