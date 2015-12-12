package main

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/pprof"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/router/types"
)

type API struct {
	router *Router
}

func apiHandler(rtr *Router) http.Handler {
	api := &API{router: rtr}
	r := httprouter.New()

	r.HandlerFunc("GET", status.Path, status.HealthyHandler.ServeHTTP)

	r.POST("/routes", httphelper.WrapHandler(api.CreateRoute))
	r.PUT("/routes/:route_type/:id", httphelper.WrapHandler(api.UpdateRoute))
	r.GET("/routes", httphelper.WrapHandler(api.GetRoutes))
	r.GET("/routes/:route_type/:id", httphelper.WrapHandler(api.GetRoute))
	r.DELETE("/routes/:route_type/:id", httphelper.WrapHandler(api.DeleteRoute))
	r.GET("/events", httphelper.WrapHandler(api.StreamEvents))

	r.HandlerFunc("GET", "/debug/*path", pprof.Handler.ServeHTTP)

	return httphelper.ContextInjector("router", httphelper.NewRequestLogger(r))
}

func (api *API) CreateRoute(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	log, _ := ctxhelper.LoggerFromContext(ctx)

	var route *router.Route
	if err := json.NewDecoder(req.Body).Decode(&route); err != nil {
		log.Error(err.Error())
		httphelper.Error(w, err)
		return
	}

	l := api.router.ListenerFor(route.Type)
	if l == nil {
		httphelper.ValidationError(w, "type", "Invalid route type")
		return
	}

	err := l.AddRoute(route)
	if err != nil {
		rjson, jerr := json.Marshal(&route)
		if jerr != nil {
			log.Error(jerr.Error())
			httphelper.Error(w, jerr)
			return
		}
		jsonError := httphelper.JSONError{Detail: rjson}
		switch err {
		case ErrConflict:
			jsonError.Code = httphelper.ConflictErrorCode
			jsonError.Message = "Duplicate route"
		case ErrInvalid:
			jsonError.Code = httphelper.ValidationErrorCode
			jsonError.Message = "Invalid route"
		default:
			log.Error(err.Error())
			httphelper.Error(w, err)
			return
		}
		httphelper.Error(w, jsonError)
		return
	}
	httphelper.JSON(w, 200, route)
}

func (api *API) UpdateRoute(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	log, _ := ctxhelper.LoggerFromContext(ctx)
	params, _ := ctxhelper.ParamsFromContext(ctx)

	var route *router.Route
	if err := json.NewDecoder(req.Body).Decode(&route); err != nil {
		log.Error(err.Error())
		httphelper.Error(w, err)
		return
	}

	route.Type = params.ByName("route_type")
	route.ID = params.ByName("id")

	l := api.router.ListenerFor(route.Type)
	if l == nil {
		httphelper.ValidationError(w, "type", "Invalid route type")
		return
	}

	if err := l.UpdateRoute(route); err != nil {
		if err == ErrNotFound {
			w.WriteHeader(404)
			return
		}
		log.Error(err.Error())
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, 200, route)
}

type sortedRoutes []*router.Route

func (p sortedRoutes) Len() int           { return len(p) }
func (p sortedRoutes) Less(i, j int) bool { return p[i].CreatedAt.After(p[j].CreatedAt) }
func (p sortedRoutes) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func (api *API) GetRoutes(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	log, _ := ctxhelper.LoggerFromContext(ctx)

	routes, err := api.router.HTTP.List()
	if err != nil {
		log.Error(err.Error())
		httphelper.Error(w, err)
		return
	}
	tcpRoutes, err := api.router.TCP.List()
	if err != nil {
		log.Error(err.Error())
		httphelper.Error(w, err)
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
	httphelper.JSON(w, 200, routes)
}

func (api *API) GetRoute(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	log, _ := ctxhelper.LoggerFromContext(ctx)
	params, _ := ctxhelper.ParamsFromContext(ctx)

	l := api.router.ListenerFor(params.ByName("route_type"))
	if l == nil {
		w.WriteHeader(404)
		return
	}

	route, err := l.Get(params.ByName("id"))
	if err == ErrNotFound {
		w.WriteHeader(404)
		return
	}
	if err != nil {
		log.Error(err.Error())
		httphelper.Error(w, err)
		return
	}

	httphelper.JSON(w, 200, route)
}

func (api *API) DeleteRoute(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	log, _ := ctxhelper.LoggerFromContext(ctx)
	params, _ := ctxhelper.ParamsFromContext(ctx)

	l := api.router.ListenerFor(params.ByName("route_type"))
	if l == nil {
		w.WriteHeader(404)
		return
	}

	err := l.RemoveRoute(params.ByName("id"))
	if err != nil {
		switch err {
		case ErrNotFound:
			w.WriteHeader(404)
			return
		case ErrInvalid:
			httphelper.Error(w, httphelper.JSONError{
				Code:    httphelper.ValidationErrorCode,
				Message: "Route has dependent routes",
			})
			return
		default:
			log.Error(err.Error())
			httphelper.Error(w, err)
			return
		}
	}
	w.WriteHeader(200)
}

func (api *API) StreamEvents(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	log, _ := ctxhelper.LoggerFromContext(ctx)

	httpListener := api.router.ListenerFor("http")
	tcpListener := api.router.ListenerFor("tcp")

	httpEvents := make(chan *router.Event)
	tcpEvents := make(chan *router.Event)
	sseEvents := make(chan *router.StreamEvent)
	go httpListener.Watch(httpEvents)
	go tcpListener.Watch(tcpEvents)
	defer httpListener.Unwatch(httpEvents)
	defer tcpListener.Unwatch(tcpEvents)
	sendEvents := func(events chan *router.Event) {
		for {
			e, ok := <-events
			if !ok {
				return
			}
			sseEvents <- &router.StreamEvent{
				Event: e.Event,
				Route: e.Route,
				Error: e.Error,
			}
		}
	}
	go sendEvents(httpEvents)
	go sendEvents(tcpEvents)
	sse.ServeStream(w, sseEvents, log)
}
