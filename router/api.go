package main

import (
	"log"
	"net/http"
	"os"
	"sort"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/martini-contrib/binding"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/martini-contrib/render"
	"github.com/flynn/flynn/pkg/pprof"
	"github.com/flynn/flynn/pkg/status"
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

	r.Get(status.Path, status.SimpleHandler(rtr.HTTP.Ping).ServeHTTP)

	r.Post("/routes", binding.Bind(router.Route{}), createRoute)
	r.Put("/routes/:route_type/:id", binding.Bind(router.Route{}), updateRoute)
	r.Get("/routes", getRoutes)
	r.Get("/routes/:route_type/:id", getRoute)
	r.Delete("/routes/:route_type/:id", deleteRoute)
	r.Any("/debug/**", pprof.Handler.ServeHTTP)
	return m
}

func createRoute(req *http.Request, route router.Route, router *Router, r render.Render) {
	l := listenerFor(router, route.Type)
	if l == nil {
		r.JSON(400, "Invalid route type")
		return
	}

	if err := l.AddRoute(&route); err != nil {
		log.Println(err)
		r.JSON(500, "unknown error")
		return
	}
	r.JSON(200, route)
}

func updateRoute(params martini.Params, route router.Route, router *Router, r render.Render) {
	route.Type = params["route_type"]
	route.ID = params["id"]

	l := listenerFor(router, route.Type)
	if l == nil {
		r.JSON(400, "Invalid route type")
		return
	}

	if err := l.UpdateRoute(&route); err != nil {
		log.Println(err)
		r.JSON(500, "unknown error")
		return
	}
	r.JSON(200, route)
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

type sortedRoutes []*router.Route

func (p sortedRoutes) Len() int           { return len(p) }
func (p sortedRoutes) Less(i, j int) bool { return p[i].CreatedAt.After(p[j].CreatedAt) }
func (p sortedRoutes) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func getRoutes(req *http.Request, rtr *Router, r render.Render) {
	routes, err := rtr.HTTP.List()
	if err != nil {
		log.Println(err)
		r.JSON(500, "unknown error")
		return
	}
	tcpRoutes, err := rtr.TCP.List()
	if err != nil {
		log.Println(err)
		r.JSON(500, "unknown error")
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

	sort.Sort(sortedRoutes(routes))
	r.JSON(200, routes)
}

func getRoute(params martini.Params, router *Router, r render.Render) {
	l := listenerFor(router, params["route_type"])
	if l == nil {
		r.JSON(404, "not found")
		return
	}

	route, err := l.Get(params["id"])
	if err == ErrNotFound {
		r.JSON(404, "not found")
		return
	}
	if err != nil {
		log.Println(err)
		r.JSON(500, "unknown error")
		return
	}

	r.JSON(200, route)
}

func deleteRoute(params martini.Params, router *Router, r render.Render) {
	l := listenerFor(router, params["route_type"])
	if l == nil {
		r.JSON(404, "not found")
		return
	}

	err := l.RemoveRoute(params["id"])
	if err == ErrNotFound {
		r.JSON(404, "not found")
		return
	}
	if err != nil {
		log.Println(err)
		r.JSON(500, "unknown error")
		return
	}

	r.JSON(200, "unknown error")
}
