package main

import (
	"sort"
	"sync"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/stream"
	routerc "github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
	. "github.com/flynn/go-check"
)

func newFakeRouter() routerc.Client {
	return &fakeRouter{routes: make(map[string]*router.Route)}
}

type fakeStream struct{}

func (s *fakeStream) Close() error { return nil }
func (s *fakeStream) Err() error   { return nil }

type fakeRouter struct {
	mtx    sync.RWMutex
	routes map[string]*router.Route
}

func (r *fakeRouter) CreateRoute(route *router.Route) error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	route.ID = route.Type + "/" + random.UUID()
	now := time.Now()
	route.CreatedAt = now
	route.UpdatedAt = now
	r.routes[route.ID] = route
	return nil
}

func (r *fakeRouter) DeleteRoute(routeType, id string) error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	if _, exists := r.routes[id]; !exists {
		return routerc.ErrNotFound
	}
	delete(r.routes, id)
	return nil
}

func (r *fakeRouter) GetRoute(routeType, id string) (*router.Route, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	route, ok := r.routes[routeType+"/"+id]
	if !ok {
		return nil, routerc.ErrNotFound
	}
	return route, nil
}

func (r *fakeRouter) UpdateRoute(route *router.Route) error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	now := time.Now()
	route.UpdatedAt = now
	r.routes[route.ID] = route
	return nil
}

func (r *fakeRouter) StreamEvents(opts *router.StreamEventsOptions, output chan *router.StreamEvent) (stream.Stream, error) {
	return &fakeStream{}, nil
}

func (r *fakeRouter) CreateCert(cert *router.Certificate) error {
	return nil
}

func (r *fakeRouter) GetCert(id string) (*router.Certificate, error) {
	return nil, nil
}

func (r *fakeRouter) DeleteCert(id string) error {
	return nil
}

func (r *fakeRouter) ListCerts() ([]*router.Certificate, error) {
	return nil, nil
}

func (r *fakeRouter) ListCertRoutes(id string) ([]*router.Route, error) {
	return nil, nil
}

type sortedRoutes []*router.Route

func (p sortedRoutes) Len() int           { return len(p) }
func (p sortedRoutes) Less(i, j int) bool { return p[i].CreatedAt.After(p[j].CreatedAt) }
func (p sortedRoutes) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func (r *fakeRouter) ListRoutes(parentRef string) ([]*router.Route, error) {
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

func (r *fakeRouter) Close() error { return nil }

func (s *S) createTestRoute(c *C, appID string, in *router.Route) *router.Route {
	c.Assert(s.c.CreateRoute(appID, in), IsNil)
	return in
}

func (s *S) TestCreateRoute(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "create-route"})
	route := s.createTestRoute(c, app.ID, (&router.TCPRoute{Service: "foo"}).ToRoute())
	c.Assert(route.ID, Not(Equals), "")

	gotRoute, err := s.c.GetRoute(app.ID, route.ID)
	c.Assert(err, IsNil)
	c.Assert(gotRoute, DeepEquals, route)
}

func (s *S) TestDeleteRoute(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "delete-route"})
	route := s.createTestRoute(c, app.ID, (&router.TCPRoute{Service: "foo"}).ToRoute())

	c.Assert(s.c.DeleteRoute(app.ID, route.ID), IsNil)

	_, err := s.c.GetRoute(app.ID, route.ID)
	c.Assert(err, Equals, controller.ErrNotFound)
}

func (s *S) TestUpdateRoute(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "update-route"})
	route0 := s.createTestRoute(c, app.ID, (&router.TCPRoute{Service: "foo"}).ToRoute())
	route1 := s.createTestRoute(c, app.ID, (&router.HTTPRoute{Service: "bar", Domain: "example.com"}).ToRoute())

	route0.Service = "foo-1"
	route1.Service = "bar-1"
	route1.Sticky = true

	c.Assert(s.c.UpdateRoute(app.ID, route0.ID, route0), IsNil)
	c.Assert(s.c.UpdateRoute(app.ID, route1.ID, route1), IsNil)

	routes, err := s.c.RouteList(app.ID)
	c.Assert(err, IsNil)

	c.Assert(routes, HasLen, 2)
	c.Assert(routes[1].ID, Equals, route0.ID)
	c.Assert(routes[1].Service, Equals, route0.Service)
	c.Assert(routes[0].ID, Equals, route1.ID)
	c.Assert(routes[0].Service, Equals, route1.Service)
	c.Assert(routes[0].Sticky, Equals, route1.Sticky)
}

func (s *S) TestListRoutes(c *C) {
	app0 := s.createTestApp(c, &ct.App{Name: "delete-route1"})
	app1 := s.createTestApp(c, &ct.App{Name: "delete-route2"})

	route0 := s.createTestRoute(c, app0.ID, (&router.TCPRoute{Service: "foo"}).ToRoute())
	route1 := s.createTestRoute(c, app0.ID, (&router.HTTPRoute{Service: "baz", Domain: "example.com"}).ToRoute())
	s.createTestRoute(c, app1.ID, (&router.TCPRoute{Service: "bar"}).ToRoute())
	s.createTestRoute(c, app1.ID, (&router.HTTPRoute{Service: "buzz", Domain: "example.net"}).ToRoute())

	routes, err := s.c.RouteList(app0.ID)
	c.Assert(err, IsNil)

	c.Assert(routes, HasLen, 2)
	c.Assert(routes[1].ID, Equals, route0.ID)
	c.Assert(routes[0].ID, Equals, route1.ID)
}
