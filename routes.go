package main

import (
	"log"
	"net/http"

	ct "github.com/flynn/flynn-controller/types"
	strowgerc "github.com/flynn/strowger/client"
	"github.com/flynn/strowger/types"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/render"
)

func createRoute(app *ct.App, router strowgerc.Client, route strowger.Route, r render.Render) {
	route.ParentRef = routeParentRef(app)
	if err := router.CreateRoute(&route); err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
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

func getRouteMiddleware(app *ct.App, c martini.Context, params martini.Params, router strowgerc.Client, w http.ResponseWriter) {
	route, err := router.GetRoute(routeID(params))
	if err == strowgerc.ErrNotFound || err == nil && route.ParentRef != routeParentRef(app) {
		w.WriteHeader(404)
		return
	}
	if err != nil {
		w.WriteHeader(500)
		return
	}
	c.Map(route)
}

func getRoute(route *strowger.Route, r render.Render) {
	r.JSON(200, route)
}

func getRouteList(app *ct.App, router strowgerc.Client, r render.Render) {
	routes, err := router.ListRoutes(routeParentRef(app))
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	r.JSON(200, routes)
}

func deleteRoute(route *strowger.Route, router strowgerc.Client, w http.ResponseWriter) {
	err := router.DeleteRoute(route.ID)
	if err == strowgerc.ErrNotFound {
		w.WriteHeader(404)
		return
	}
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(200)
}
