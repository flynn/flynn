package main

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
	ct "github.com/flynn/flynn/controller/types"
	routerc "github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
)

func createRoute(app *ct.App, router routerc.Client, route router.Route, r ResponseHelper) {
	route.ParentRef = routeParentRef(app)
	if err := router.CreateRoute(&route); err != nil {
		r.Error(err)
		return
	}
	r.JSON(200, &route)
}

func routeID(params martini.Params) string {
	return params["routes_type"] + "/" + params["routes_id"]
}

func routeParentRef(app *ct.App) string {
	return "controller/apps/" + app.ID
}

func getRouteMiddleware(app *ct.App, c martini.Context, params martini.Params, router routerc.Client, r ResponseHelper) {
	route, err := router.GetRoute(routeID(params))
	if err == routerc.ErrNotFound || err == nil && route.ParentRef != routeParentRef(app) {
		err = ErrNotFound
	}
	if err != nil {
		r.Error(err)
		return
	}
	c.Map(route)
}

func getRoute(route *router.Route, r ResponseHelper) {
	r.JSON(200, route)
}

func getRouteList(app *ct.App, router routerc.Client, r ResponseHelper) {
	routes, err := router.ListRoutes(routeParentRef(app))
	if err != nil {
		r.Error(err)
		return
	}
	r.JSON(200, routes)
}

func deleteRoute(route *router.Route, router routerc.Client, r ResponseHelper) {
	err := router.DeleteRoute(route.ID)
	if err == routerc.ErrNotFound {
		err = ErrNotFound
	}
	if err != nil {
		r.Error(err)
		return
	}
	r.WriteHeader(200)
}
