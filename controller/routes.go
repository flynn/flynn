package main

import (
	"net/http"

	"github.com/flynn/flynn/controller/data"
	"github.com/flynn/flynn/pkg/httphelper"
	routerc "github.com/flynn/flynn/router/client"
	router "github.com/flynn/flynn/router/types"
	"golang.org/x/net/context"
)

func (c *controllerAPI) CreateRoute(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var route router.Route
	if err := httphelper.DecodeJSON(req, &route); err != nil {
		respondWithError(w, err)
		return
	}

	if err := data.CreateRoute(c.config.db, c.routerc, c.getApp(ctx).ID, &route); err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, &route)
}

func (c *controllerAPI) GetRoute(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	route, err := c.getRoute(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, route)
}

func (c *controllerAPI) GetRouteList(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	routes, err := c.routerc.ListRoutes(routeParentRef(c.getApp(ctx).ID))
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, routes)
}

func (c *controllerAPI) UpdateRoute(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var route *router.Route
	if err := httphelper.DecodeJSON(req, &route); err != nil {
		respondWithError(w, err)
		return
	}

	err := c.routerc.UpdateRoute(route)
	if err == routerc.ErrNotFound {
		err = ErrNotFound
	}
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, route)
}

func (c *controllerAPI) DeleteRoute(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	route, err := c.getRoute(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	err = c.routerc.DeleteRoute(route.Type, route.ID)
	if err == routerc.ErrNotFound {
		err = ErrNotFound
	}
	if err != nil {
		respondWithError(w, err)
		return
	}
	w.WriteHeader(200)
}
