package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/martini-contrib/binding"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/martini-contrib/render"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/router/types"
)

func apiHandler(rtr *Router) http.Handler {
	r := martini.NewRouter()
	m := martini.New()
	m.Map(log.New(os.Stdout, "[router] ", log.LstdFlags|log.Lmicroseconds))
	m.Use(martini.Logger())
	m.Use(martini.Recovery())
	m.Use(render.Renderer())
	m.Action(r.Handle)
	m.Map(rtr)

	r.Post("/routes", binding.Bind(router.Route{}), createRoute)
	r.Put("/routes", binding.Bind(router.Route{}), createOrReplaceRoute)
	r.Get("/routes", getRoutes)
	r.Get("/routes/:route_type/:route_id", getRoute)
	r.Delete("/routes/:route_type/:route_id", deleteRoute)
	r.Put("/services/:service_type/:service_name", binding.Json(router.PauseReq{}), pauseService)
	r.Get("/services/:service_type/:service_name/drain", streamServiceDrain)
	return m
}

func createRoute(req *http.Request, route router.Route, router *Router, r render.Render) {
	now := time.Now()
	route.CreatedAt = &now
	route.UpdatedAt = &now

	l := listenerFor(router, route.Type)
	if l == nil {
		r.JSON(400, "Invalid route type")
		return
	}

	if err := l.AddRoute(&route); err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	res := formatRoute(&route)
	r.JSON(200, res)
}

func createOrReplaceRoute(req *http.Request, route router.Route, router *Router, r render.Render) {
	now := time.Now()
	route.CreatedAt = &now
	route.UpdatedAt = &now

	l := listenerFor(router, route.Type)
	if l == nil {
		r.JSON(400, "Invalid route type")
		return
	}

	if err := l.SetRoute(&route); err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	res := formatRoute(&route)
	r.JSON(200, res)
}

func listenerFor(router *Router, typ string) Listener {
	switch typ {
	case "http":
		return router.HTTP
	case "tcp":
		return router.TCP
	default:
		return nil
	}
}

func formatRoute(r *router.Route) *router.Route {
	r.ID = fmt.Sprintf("%s/%s", r.Type, r.ID)
	switch r.Type {
	case "http":
		httpRoute := r.HTTPRoute()
		httpRoute.TLSKey = ""
		httpRoute.Route = nil
		conf, _ := json.Marshal(httpRoute)
		jsonConf := json.RawMessage(conf)
		r.Config = &jsonConf
	}
	return r
}

type sortedRoutes []*router.Route

func (p sortedRoutes) Len() int           { return len(p) }
func (p sortedRoutes) Less(i, j int) bool { return p[i].CreatedAt.After(*p[j].CreatedAt) }
func (p sortedRoutes) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func getRoutes(req *http.Request, rtr *Router, r render.Render) {
	routes, err := rtr.HTTP.List()
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	tcpRoutes, err := rtr.TCP.List()
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	routes = append(routes, tcpRoutes...)

	if ref := req.URL.Query().Get("parent_ref"); ref != "" {
		filtered := make([]*router.Route, 0)
		for _, route := range routes {
			if route.ParentRef == ref {
				filtered = append(filtered, route)
			}
		}
		routes = filtered
	}
	for i, route := range routes {
		routes[i] = formatRoute(route)
	}

	sort.Sort(sortedRoutes(routes))
	r.JSON(200, routes)
}

func getRoute(params martini.Params, router *Router, r render.Render) {
	l := listenerFor(router, params["route_type"])
	if l == nil {
		r.JSON(404, struct{}{})
		return
	}

	route, err := l.Get(params["route_id"])
	if err == ErrNotFound {
		r.JSON(404, struct{}{})
		return
	}
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}

	r.JSON(200, formatRoute(route))
}

func deleteRoute(params martini.Params, router *Router, r render.Render) {
	l := listenerFor(router, params["route_type"])
	if l == nil {
		r.JSON(404, struct{}{})
		return
	}

	err := l.RemoveRoute(params["route_id"])
	if err == ErrNotFound {
		r.JSON(404, struct{}{})
		return
	}
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}

	r.JSON(200, struct{}{})
}

func pauseService(req *http.Request, pauseReq router.PauseReq, params martini.Params, router *Router, r render.Render) {
	l := listenerFor(router, params["service_type"])
	if l == nil {
		r.JSON(404, struct{}{})
		return
	}
	err := l.PauseService(params["service_name"], pauseReq.Paused)
	if err == ErrNotFound {
		r.JSON(404, struct{}{})
		return
	}
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}

	r.JSON(200, struct{}{})
}

func streamServiceDrain(req *http.Request, params martini.Params, router *Router, w http.ResponseWriter) {
	l := listenerFor(router, params["service_type"])
	if l == nil {
		w.WriteHeader(404)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.WriteHeader(200)
	if wf, ok := w.(http.Flusher); ok {
		wf.Flush()
	}

	ch := make(chan string)
	l.AddDrainListener(params["service_name"], ch)
	defer l.RemoveDrainListener(params["service_name"], ch)

	ssew := sse.NewSSEWriter(w)
	for event := range ch {
		if _, err := ssew.Write([]byte(event)); err != nil {
			return
		}
		ssew.Flush()
	}
}
