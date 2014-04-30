package main

import (
	ct "github.com/flynn/flynn-controller/types"
	strowgerc "github.com/flynn/strowger/client"
	"github.com/flynn/strowger/types"
	"github.com/go-martini/martini"
)

func createRoute(app *ct.App, router strowgerc.Client, route strowger.Route, r ResponseHelper) {
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

func getRouteMiddleware(app *ct.App, c martini.Context, params martini.Params, router strowgerc.Client, r ResponseHelper) {
	route, err := router.GetRoute(routeID(params))
	if err == strowgerc.ErrNotFound || err == nil && route.ParentRef != routeParentRef(app) {
		err = ErrNotFound
	}
	if err != nil {
		r.Error(err)
		return
	}
	c.Map(route)
}

func getRoute(route *strowger.Route, r ResponseHelper) {
	r.JSON(200, route)
}

func getRouteList(app *ct.App, router strowgerc.Client, r ResponseHelper) {
	routes, err := router.ListRoutes(routeParentRef(app))
	if err != nil {
		r.Error(err)
		return
	}
	r.JSON(200, routes)
}

func deleteRoute(route *strowger.Route, router strowgerc.Client, r ResponseHelper) {
	err := router.DeleteRoute(route.ID)
	if err == strowgerc.ErrNotFound {
		err = ErrNotFound
	}
	if err != nil {
		r.Error(err)
		return
	}
	r.WriteHeader(200)
}
