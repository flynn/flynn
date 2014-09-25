package main

import (
	"bufio"
	"net/http"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/sse"
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

func pauseService(pauseReq router.PauseReq, params martini.Params, r ResponseHelper, req *http.Request) {
	services, err := discoverd.Services("router-api", time.Second)
	if err != nil {
		r.Error(err)
		return
	}
	done := make(chan error, len(services))
	for _, service := range services {
		go func() {
			router := routerc.NewWithAddr(service.Addr)
			defer router.Close()
			if err := router.PauseService(params["service_type"], params["service_name"], pauseReq.Paused); err != nil {
				done <- err
				return
			}
			done <- nil
		}()
	}
	for i := 0; i < len(services); i++ {
		err := <-done
		if err != nil {
			r.Error(err)
			return
		}
	}
	r.WriteHeader(200)
}

func streamServiceDrain(req *http.Request, params martini.Params, r ResponseHelper, w http.ResponseWriter) {
	services, err := discoverd.Services("router-api", time.Second)
	if err != nil {
		r.Error(err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	if wf, ok := w.(http.Flusher); ok {
		wf.Flush()
	}
	done := make(chan error, len(services))
	for _, service := range services {
		go func() {
			router := routerc.NewWithAddr(service.Addr)
			defer router.Close()
			stream, err := router.StreamServiceDrain(params["service_type"], params["service_name"])
			defer stream.Close()
			if err != nil {
				done <- err
				return
			}
			dec := &sse.Reader{bufio.NewReader(stream)}
			for {
				line, err := dec.Read()
				if err != nil {
					done <- err
					return
				}
				if string(line) == "all" {
					done <- nil
					break
				}
			}
		}()
	}
	for i := 0; i < len(services); i++ {
		err := <-done
		if err != nil {
			r.Error(err)
			return
		}
	}
	r.WriteHeader(200)
}
