package main

import (
	"io"
	"net/http"

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

func pauseService(router routerc.Client, params martini.Params, r ResponseHelper, req *http.Request) {
	pause := false
	if req.FormValue("pause") == "true" {
		pause = true
	}
	err := router.PauseService(params["service_type"], params["service_name"], pause)
	if err != nil {
		r.Error(err)
		return
	}
	r.WriteHeader(200)
}

func streamServiceDrain(req *http.Request, params martini.Params, router routerc.Client, r ResponseHelper, w http.ResponseWriter) {
	stream, err := router.StreamServiceDrain(params["service_type"], params["service_name"])
	defer stream.Close()
	if err != nil {
		r.Error(err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.WriteHeader(200)
	if wf, ok := w.(http.Flusher); ok {
		wf.Flush()
	}
	if _, err := io.Copy(w, stream); err != nil {
		r.Error(err)
	}
}
