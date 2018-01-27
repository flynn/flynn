package main

import (
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/router/types"
	. "github.com/flynn/go-check"
)

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
