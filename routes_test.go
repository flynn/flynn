package main

import (
	"fmt"
	"sort"
	"sync"
	"time"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-controller/utils"
	strowgerc "github.com/flynn/strowger/client"
	"github.com/flynn/strowger/types"
	. "github.com/titanous/gocheck"
)

func newFakeRouter() strowgerc.Client {
	return &fakeRouter{routes: make(map[string]*strowger.Route)}
}

type fakeRouter struct {
	mtx    sync.RWMutex
	routes map[string]*strowger.Route
}

func (r *fakeRouter) CreateRoute(route *strowger.Route) error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	route.ID = route.Type + "/" + utils.UUID()
	now := time.Now()
	route.CreatedAt = &now
	route.UpdatedAt = &now
	r.routes[route.ID] = route
	return nil
}

func (r *fakeRouter) DeleteRoute(id string) error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	if _, exists := r.routes[id]; !exists {
		return strowgerc.ErrNotFound
	}
	delete(r.routes, id)
	return nil
}

func (r *fakeRouter) GetRoute(id string) (*strowger.Route, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	route, ok := r.routes[id]
	if !ok {
		return nil, strowgerc.ErrNotFound
	}
	return route, nil
}

type sortedRoutes []*strowger.Route

func (p sortedRoutes) Len() int           { return len(p) }
func (p sortedRoutes) Less(i, j int) bool { return p[i].CreatedAt.After(*p[j].CreatedAt) }
func (p sortedRoutes) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func (r *fakeRouter) ListRoutes(parentRef string) ([]*strowger.Route, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	routes := make([]*strowger.Route, 0, len(r.routes))
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

func (s *S) createTestRoute(c *C, appID string, in *strowger.Route) *strowger.Route {
	out := &strowger.Route{}
	res, err := s.Post(fmt.Sprintf("/apps/%s/routes", appID), in, out)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	return out
}

func (s *S) TestCreateRoute(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "create-route"})
	route := s.createTestRoute(c, app.ID, (&strowger.TCPRoute{Service: "foo"}).ToRoute())
	c.Assert(route.ID, Not(Equals), "")

	gotRoute := &strowger.Route{}
	path := fmt.Sprintf("/apps/%s/routes/%s", app.ID, route.ID)
	_, err := s.Get(path, gotRoute)
	c.Assert(err, IsNil)
	c.Assert(gotRoute, DeepEquals, route)
}

func (s *S) TestDeleteRoute(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "delete-route"})
	route := s.createTestRoute(c, app.ID, (&strowger.TCPRoute{Service: "foo"}).ToRoute())

	path := fmt.Sprintf("/apps/%s/routes/%s", app.ID, route.ID)
	res, err := s.Delete(path)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	res, err = s.Get(path, route)
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) TestListRoutes(c *C) {
	app0 := s.createTestApp(c, &ct.App{Name: "delete-route1"})
	app1 := s.createTestApp(c, &ct.App{Name: "delete-route2"})

	route0 := s.createTestRoute(c, app0.ID, (&strowger.TCPRoute{Service: "foo"}).ToRoute())
	route1 := s.createTestRoute(c, app0.ID, (&strowger.HTTPRoute{Service: "baz", Domain: "example.com"}).ToRoute())
	s.createTestRoute(c, app1.ID, (&strowger.TCPRoute{Service: "bar"}).ToRoute())
	s.createTestRoute(c, app1.ID, (&strowger.HTTPRoute{Service: "buzz", Domain: "example.net"}).ToRoute())

	var routes []*strowger.Route
	res, err := s.Get(fmt.Sprintf("/apps/%s/routes", app0.ID), &routes)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	c.Assert(routes, HasLen, 2)
	c.Assert(routes[1].ID, Equals, route0.ID)
	c.Assert(routes[0].ID, Equals, route1.ID)
}
