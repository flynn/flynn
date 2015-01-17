package main

import (
	"encoding/json"
	"net/http"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/pkg/httphelper"
	routerc "github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
)

func (c *controllerAPI) CreateRoute(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	app, err := c.getApp(params)
	if err != nil {
		respondWithError(w, err)
		return
	}

	var route router.Route
	dec := json.NewDecoder(req.Body)
	err = dec.Decode(&route)
	if err != nil {
		respondWithError(w, err)
		return
	}

	route.ParentRef = routeParentRef(app.ID)
	if err := c.routerc.CreateRoute(&route); err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &route)
}

func (c *controllerAPI) GetRoute(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	app, err := c.getApp(params)
	if err != nil {
		respondWithError(w, err)
		return
	}

	route, err := c.getRoute(app.ID, params)
	if err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, route)
}

func (c *controllerAPI) GetRouteList(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	app, err := c.getApp(params)
	if err != nil {
		respondWithError(w, err)
		return
	}

	routes, err := c.routerc.ListRoutes(routeParentRef(app.ID))
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, routes)
}

func (c *controllerAPI) DeleteRoute(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	app, err := c.getApp(params)
	if err != nil {
		respondWithError(w, err)
		return
	}

	route, err := c.getRoute(app.ID, params)
	if err != nil {
		respondWithError(w, err)
		return
	}

	err = c.routerc.DeleteRoute(route.ID)
	if err == routerc.ErrNotFound {
		err = ErrNotFound
	}
	if err != nil {
		respondWithError(w, err)
		return
	}
	w.WriteHeader(200)
}
