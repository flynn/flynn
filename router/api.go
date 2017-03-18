package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/pprof"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/router/types"
	"github.com/julienschmidt/httprouter"
	"golang.org/x/net/context"
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
	r.POST("/certificates", httphelper.WrapHandler(api.CreateCert))
	r.GET("/certificates/:id", httphelper.WrapHandler(api.GetCert))
	r.GET("/certificates/:id/routes", httphelper.WrapHandler(api.GetCertRoutes))
	r.DELETE("/certificates/:id", httphelper.WrapHandler(api.DeleteCert))
	r.GET("/certificates", httphelper.WrapHandler(api.GetCerts))
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
		case ErrReserved:
			jsonError.Code = httphelper.ConflictErrorCode
			jsonError.Message = "Port reserved for HTTP/HTTPS traffic"
		case ErrUnreservedHTTP:
			jsonError.Code = httphelper.ValidationErrorCode
			jsonError.Message = "Port not reserved for HTTP traffic"
		case ErrUnreservedHTTPS:
			jsonError.Code = httphelper.ValidationErrorCode
			jsonError.Message = "Port not reserved for HTTPS traffic"
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

func (api *API) CreateCert(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var cert *router.Certificate
	if err := json.NewDecoder(req.Body).Decode(&cert); err != nil {
		httphelper.Error(w, err)
		return
	}

	l := api.router.HTTP.(*HTTPListener)
	err := l.AddCert(cert)
	if err != nil {
		jsonError := httphelper.JSONError{}
		switch err {
		case ErrConflict:
			jsonError.Code = httphelper.ConflictErrorCode
			jsonError.Message = "Duplicate cert"
		case ErrInvalid:
			jsonError.Code = httphelper.ValidationErrorCode
			jsonError.Message = "Invalid cert"
		default:
			httphelper.Error(w, err)
			return
		}
	}
	httphelper.JSON(w, 200, cert)
}

func (api *API) GetCert(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	l := api.router.HTTP.(*HTTPListener)
	cert, err := l.GetCert(params.ByName("id"))
	if err == ErrNotFound {
		httphelper.ObjectNotFoundError(w, "certificate not found")
		return
	}
	if err != nil {
		httphelper.Error(w, err)
		return
	}

	httphelper.JSON(w, 200, cert)
}

func (api *API) GetCertRoutes(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	l := api.router.HTTP.(*HTTPListener)
	routes, err := l.GetCertRoutes(params.ByName("id"))
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, 200, routes)
}

func (api *API) GetCerts(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	l := api.router.HTTP.(*HTTPListener)
	certs, err := l.GetCerts()
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, 200, certs)
}

func (api *API) DeleteCert(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	l := api.router.HTTP.(*HTTPListener)
	err := l.RemoveCert(params.ByName("id"))
	if err != nil {
		switch err {
		case ErrNotFound:
			httphelper.ObjectNotFoundError(w, "certificate not found")
			return
		default:
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
	go httpListener.Watch(httpEvents, true)
	go tcpListener.Watch(tcpEvents, true)
	defer httpListener.Unwatch(httpEvents)
	defer tcpListener.Unwatch(tcpEvents)

	reqTypes := strings.Split(req.URL.Query().Get("types"), ",")
	eventTypes := make(map[router.EventType]struct{}, len(reqTypes))
	for _, typ := range reqTypes {
		eventTypes[router.EventType(typ)] = struct{}{}
	}

	sendEvents := func(events chan *router.Event) {
		for {
			e, ok := <-events
			if !ok {
				return
			}
			if _, ok := eventTypes[e.Event]; !ok {
				continue
			}
			sseEvents <- &router.StreamEvent{
				Event:   e.Event,
				Route:   e.Route,
				Backend: e.Backend,
				Error:   e.Error,
			}
		}
	}
	go sendEvents(httpEvents)
	go sendEvents(tcpEvents)
	sse.ServeStream(w, sseEvents, log)
}
