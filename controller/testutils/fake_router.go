package testutils

import (
	"sort"
	"sync"
	"time"

	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/stream"
	routerc "github.com/flynn/flynn/router/client"
	router "github.com/flynn/flynn/router/types"
)

func NewFakeRouter() routerc.Client {
	return &FakeRouter{routes: make(map[string]*router.Route)}
}

type FakeStream struct{}

func (s *FakeStream) Close() error { return nil }
func (s *FakeStream) Err() error   { return nil }

type FakeRouter struct {
	mtx    sync.RWMutex
	routes map[string]*router.Route
}

func (r *FakeRouter) CreateRoute(route *router.Route) error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	route.ID = route.Type + "/" + random.UUID()
	now := time.Now()
	route.CreatedAt = now
	route.UpdatedAt = now
	r.routes[route.ID] = route
	return nil
}

func (r *FakeRouter) DeleteRoute(routeType, id string) error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	if _, exists := r.routes[id]; !exists {
		return routerc.ErrNotFound
	}
	delete(r.routes, id)
	return nil
}

func (r *FakeRouter) GetRoute(routeType, id string) (*router.Route, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	route, ok := r.routes[routeType+"/"+id]
	if !ok {
		return nil, routerc.ErrNotFound
	}
	return route, nil
}

func (r *FakeRouter) UpdateRoute(route *router.Route) error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	now := time.Now()
	route.UpdatedAt = now
	r.routes[route.ID] = route
	return nil
}

func (r *FakeRouter) StreamEvents(opts *router.StreamEventsOptions, output chan *router.StreamEvent) (stream.Stream, error) {
	return &FakeStream{}, nil
}

func (r *FakeRouter) CreateCert(cert *router.Certificate) error {
	return nil
}

func (r *FakeRouter) GetCert(id string) (*router.Certificate, error) {
	return nil, nil
}

func (r *FakeRouter) DeleteCert(id string) error {
	return nil
}

func (r *FakeRouter) ListCerts() ([]*router.Certificate, error) {
	return nil, nil
}

func (r *FakeRouter) ListCertRoutes(id string) ([]*router.Route, error) {
	return nil, nil
}

type sortedRoutes []*router.Route

func (p sortedRoutes) Len() int           { return len(p) }
func (p sortedRoutes) Less(i, j int) bool { return p[i].CreatedAt.After(p[j].CreatedAt) }
func (p sortedRoutes) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func (r *FakeRouter) ListRoutes(parentRef string) ([]*router.Route, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	routes := make([]*router.Route, 0, len(r.routes))
	for _, route := range r.routes {
		if parentRef != "" && route.ParentRef != parentRef {
			continue
		}
		routes = append(routes, route)
	}
	sort.Sort(sortedRoutes(routes))
	return routes, nil
}

func (r *FakeRouter) Close() error { return nil }
