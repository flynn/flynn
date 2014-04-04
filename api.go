package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/flynn/strowger/types"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
)

func apiHandler(router *Router) http.Handler {
	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Logger())
	m.Use(martini.Recovery())
	m.Use(render.Renderer())
	m.Action(r.Handle)
	m.Map(router)

	r.Post("/routes", binding.Bind(strowger.Route{}), createRoute)
	r.Get("/routes", getRoutes)
	r.Get("/routes/:route_type/:route_id", getRoute)
	r.Delete("/routes/:route_type/:route_id", deleteRoute)
	return m
}

func createRoute(req *http.Request, route strowger.Route, router *Router, r render.Render) {
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

func formatRoute(r *strowger.Route) *strowger.Route {
	r.ID = fmt.Sprintf("/routes/%s/%s", r.Type, r.ID)
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

type sortedRoutes []*strowger.Route

func (p sortedRoutes) Len() int           { return len(p) }
func (p sortedRoutes) Less(i, j int) bool { return p[i].CreatedAt.After(*p[j].CreatedAt) }
func (p sortedRoutes) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func getRoutes(req *http.Request, router *Router, r render.Render) {
	routes, err := router.HTTP.List()
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	tcpRoutes, err := router.TCP.List()
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	routes = append(routes, tcpRoutes...)

	if ref := req.URL.Query().Get("parent_ref"); ref != "" {
		filtered := make([]*strowger.Route, 0)
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
