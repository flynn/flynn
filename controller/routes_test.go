package main

import (
	"encoding/json"
	"strings"
	"time"

	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/tlscert"
	"github.com/flynn/flynn/router/testutils"
	router "github.com/flynn/flynn/router/types"
	. "github.com/flynn/go-check"
)

type fakeStream struct{}

func (s *fakeStream) Close() error { return nil }
func (s *fakeStream) Err() error   { return nil }

func (s *S) createTestRoute(c *C, appID string, in *router.Route) *router.Route {
	c.Assert(s.c.CreateRoute(appID, in), IsNil)
	return in
}

func (s *S) TestCreateTCPRoute(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "create-tcp-route"})
	route := s.createTestRoute(c, app.ID, (&router.TCPRoute{Service: "foo"}).ToRoute())
	c.Assert(route.ID, Not(Equals), "")

	tcpRoute := route.TCPRoute()
	c.Assert(tcpRoute.ID, Not(Equals), "")
	c.Assert(tcpRoute.CreatedAt, Not(IsNil))
	c.Assert(tcpRoute.UpdatedAt, Not(IsNil))
	c.Assert(tcpRoute.Service, Equals, "foo")
	c.Assert(tcpRoute.Port, Not(Equals), 0)
	c.Assert(tcpRoute.Leader, Equals, false)

	gotRoute, err := s.c.GetRoute(app.ID, route.FormattedID())
	c.Assert(err, IsNil)
	c.Assert(gotRoute, DeepEquals, route)
}

func (s *S) TestCreateHTTPRoute(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "create-http-route"})
	route := s.createTestRoute(c, app.ID, (&router.HTTPRoute{Domain: "create.example.com", Service: "foo"}).ToRoute())
	c.Assert(route.ID, Not(Equals), "")

	httpRoute := route.HTTPRoute()
	c.Assert(httpRoute.ID, Not(Equals), "")
	c.Assert(httpRoute.CreatedAt, Not(IsNil))
	c.Assert(httpRoute.UpdatedAt, Not(IsNil))
	c.Assert(httpRoute.Service, Equals, "foo")
	c.Assert(httpRoute.Domain, Equals, "create.example.com")
	c.Assert(httpRoute.Path, Equals, "/")
	c.Assert(httpRoute.Sticky, Equals, false)
	c.Assert(httpRoute.Leader, Equals, false)

	gotRoute, err := s.c.GetRoute(app.ID, route.FormattedID())
	c.Assert(err, IsNil)
	c.Assert(gotRoute, DeepEquals, route)
}

func (s *S) TestCreateHTTPRouteWithCertificate(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "create-http-route-with-certificate"})
	tlsCert := testutils.TLSConfigForDomain("tls.example.com")
	route := s.createTestRoute(c, app.ID, (&router.HTTPRoute{
		Domain:  "tls.example.com",
		Service: "foo",
		Certificate: &router.Certificate{
			Cert: tlsCert.Cert,
			Key:  tlsCert.PrivateKey,
		},
	}).ToRoute())
	c.Assert(route.ID, Not(Equals), "")
	c.Assert(route.Domain, Equals, "tls.example.com")
	c.Assert(route.Certificate, Not(IsNil))
	c.Assert(route.Certificate.ID, Not(Equals), "")
	c.Assert(route.Certificate.Cert, Equals, tlsCert.Cert)
	c.Assert(route.Certificate.Key, Equals, tlsCert.PrivateKey)
	c.Assert(route.Certificate.CreatedAt, Not(IsNil))
	c.Assert(route.Certificate.UpdatedAt, Not(IsNil))

	gotRoute, err := s.c.GetRoute(app.ID, route.FormattedID())
	c.Assert(err, IsNil)
	c.Assert(gotRoute.Certificate, Not(IsNil))
	c.Assert(gotRoute.Certificate, DeepEquals, route.Certificate)
}

func (s *S) TestCreateHTTPRouteWithInvalidCertificate(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "create-http-route-with-invalid-certificate"})
	c1, _ := tlscert.Generate([]string{"1.example.com"})
	c2, _ := tlscert.Generate([]string{"2.example.com"})
	err := s.c.CreateRoute(app.ID, router.HTTPRoute{
		Domain:  "tls-invalid.example.com",
		Service: "foo",
		Certificate: &router.Certificate{
			Cert: c1.Cert,
			Key:  c2.PrivateKey,
		},
	}.ToRoute())
	c.Assert(err, Not(IsNil))
}

func (s *S) TestCreateHTTPRouteWithPath(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "create-http-route-with-invalid-path"})

	// create a default route
	route1 := router.HTTPRoute{
		Domain:  "foo.bar",
		Service: "foo",
	}.ToRoute()
	c.Assert(s.c.CreateRoute(app.ID, route1), IsNil)

	// Test that adding a route with a path below the default route succeeds
	route2 := router.HTTPRoute{
		Domain:  "foo.bar",
		Service: "bar",
		Path:    "/bar/",
	}.ToRoute()
	c.Assert(s.c.CreateRoute(app.ID, route2), IsNil)

	// Test that a path with no trailing slash will autocorrect
	route3 := router.HTTPRoute{
		Domain:  "foo.bar",
		Service: "baz",
		Path:    "/baz",
	}.ToRoute()
	c.Assert(s.c.CreateRoute(app.ID, route3), IsNil)
	c.Assert(route3.Path, Equals, "/baz/")

	// Test that adding a route with an invalid path errors
	err := s.c.CreateRoute(app.ID, router.HTTPRoute{
		Domain:  "foo.bar",
		Service: "foo",
		Path:    "noleadingslash/",
	}.ToRoute())
	c.Assert(err, Not(IsNil))

	// Test that adding a Path route without a default route fails
	err = s.c.CreateRoute(app.ID, router.HTTPRoute{
		Domain:  "foo.bar.baz",
		Service: "foo",
		Path:    "/valid/",
	}.ToRoute())
	c.Assert(err, Not(IsNil))

	// Test that removing the default route while there are still dependent routes fails
	err = s.c.DeleteRoute(app.ID, route1.FormattedID())
	c.Assert(err, Not(IsNil))

	// However removing them in the appropriate order should succeed
	c.Assert(s.c.DeleteRoute(app.ID, route2.FormattedID()), IsNil)
	c.Assert(s.c.DeleteRoute(app.ID, route3.FormattedID()), IsNil)
	c.Assert(s.c.DeleteRoute(app.ID, route1.FormattedID()), IsNil)
}

func (s *S) TestCreateDuplicateRoute(c *C) {
	// first create route
	app := s.createTestApp(c, &ct.App{Name: "create-duplicate-route"})
	route := s.createTestRoute(c, app.ID, (&router.HTTPRoute{Domain: "dup.example.com", Service: "foo"}).ToRoute())

	// ensure we got back what we expect
	httpRoute := route.HTTPRoute()
	c.Assert(httpRoute.ID, Not(Equals), "")
	c.Assert(httpRoute.CreatedAt, Not(IsNil))
	c.Assert(httpRoute.UpdatedAt, Not(IsNil))
	c.Assert(httpRoute.Service, Equals, "foo")
	c.Assert(httpRoute.Domain, Equals, "dup.example.com")
	c.Assert(httpRoute.Sticky, Equals, false)
	c.Assert(httpRoute.Leader, Equals, false)

	// attempt to create the same route again, ensure fails with conflict
	err := s.c.CreateRoute(app.ID, route)
	c.Assert(err, Not(IsNil))
	c.Assert(err.Error(), Equals, "conflict: Duplicate route")
}

func (s *S) TestCreateTCPRouteReservedPort(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "create-tcp-route-reserved-port"})

	reservedPorts := []int{80, 443}

	for _, port := range reservedPorts {
		err := s.c.CreateRoute(app.ID, router.TCPRoute{
			Port: port,
		}.ToRoute())
		c.Assert(err, NotNil)
		c.Assert(err.Error(), Equals, "conflict: Port reserved for HTTP/HTTPS traffic")
	}
}

func (s *S) TestDeleteRoute(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "delete-route"})
	route := s.createTestRoute(c, app.ID, (&router.TCPRoute{Service: "foo"}).ToRoute())

	c.Assert(s.c.DeleteRoute(app.ID, route.FormattedID()), IsNil)

	_, err := s.c.GetRoute(app.ID, route.FormattedID())
	c.Assert(err, Equals, controller.ErrNotFound)
}

func (s *S) TestUpdateRoute(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "update-route"})
	route0 := s.createTestRoute(c, app.ID, (&router.TCPRoute{Service: "foo"}).ToRoute())
	route1 := s.createTestRoute(c, app.ID, (&router.HTTPRoute{Service: "bar", Domain: "update.example.com"}).ToRoute())

	route0.Service = "foo-1"
	route1.Service = "bar-1"
	route1.Sticky = true

	c.Assert(s.c.UpdateRoute(app.ID, route0.FormattedID(), route0), IsNil)
	c.Assert(s.c.UpdateRoute(app.ID, route1.FormattedID(), route1), IsNil)

	routes, err := s.c.AppRouteList(app.ID)
	c.Assert(err, IsNil)

	c.Assert(routes, HasLen, 2)
	c.Assert(routes[1].ID, Equals, route0.ID)
	c.Assert(routes[1].Service, Equals, route0.Service)
	c.Assert(routes[0].ID, Equals, route1.ID)
	c.Assert(routes[0].Service, Equals, route1.Service)
	c.Assert(routes[0].Sticky, Equals, route1.Sticky)
}

func (s *S) TestListRoutes(c *C) {
	app0 := s.createTestApp(c, &ct.App{Name: "list-route1"})
	app1 := s.createTestApp(c, &ct.App{Name: "list-route2"})

	r0 := s.createTestRoute(c, app0.ID, (&router.HTTPRoute{Domain: "list.example.com", Service: "test"}).ToRoute())
	r1 := s.createTestRoute(c, app0.ID, (&router.HTTPRoute{Domain: "list.example.net", Service: "test"}).ToRoute())
	r2 := s.createTestRoute(c, app0.ID, (&router.TCPRoute{Service: "test"}).ToRoute())
	r3 := s.createTestRoute(c, app0.ID, (&router.TCPRoute{Service: "test"}).ToRoute())

	tlsCert := testutils.TLSConfigForDomain("*.bar.example.org")
	r4 := s.createTestRoute(c, app0.ID, (&router.HTTPRoute{
		Domain:  "1.bar.example.org",
		Service: "test",
		Certificate: &router.Certificate{
			Cert: tlsCert.Cert,
			Key:  tlsCert.PrivateKey,
		},
	}).ToRoute())

	r5 := s.createTestRoute(c, app1.ID, (&router.TCPRoute{Service: "bar"}).ToRoute())
	r6 := s.createTestRoute(c, app1.ID, (&router.HTTPRoute{Service: "buzz", Domain: "list.example.org"}).ToRoute())

	routes, err := s.c.AppRouteList(app0.ID)
	c.Assert(err, IsNil)

	c.Assert(routes, HasLen, 5)
	c.Assert(routes[4].ID, Equals, r0.ID)
	c.Assert(routes[3].ID, Equals, r1.ID)
	c.Assert(routes[2].ID, Equals, r2.ID)
	c.Assert(routes[1].ID, Equals, r3.ID)
	c.Assert(routes[0].ID, Equals, r4.ID)

	c.Assert(routes[0].Certificate, Not(IsNil))
	c.Assert(routes[0].Certificate.Cert, Equals, strings.TrimSuffix(tlsCert.Cert, "\n"))
	c.Assert(routes[0].Certificate.Key, Equals, strings.TrimSuffix(tlsCert.PrivateKey, "\n"))

	routes, err = s.c.AppRouteList(app1.ID)
	c.Assert(err, IsNil)

	c.Assert(routes, HasLen, 2)
	c.Assert(routes[1].ID, Equals, r5.ID)
	c.Assert(routes[0].ID, Equals, r6.ID)
}

func (s *S) TestStreamRouteEvents(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "stream-route-events"})

	events := make(chan *ct.Event)
	stream, err := s.c.StreamEvents(ct.StreamEventsOptions{
		AppID: app.ID,
		ObjectTypes: []ct.EventType{
			ct.EventTypeRoute,
			ct.EventTypeRouteDeletion,
		},
	}, events)
	c.Assert(err, IsNil)
	defer stream.Close()

	route := s.createTestRoute(c, app.ID, (&router.HTTPRoute{Domain: "stream.example.com", Service: "foo"}).ToRoute())
	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		c.Assert(e.ObjectType, Equals, ct.EventTypeRoute)
		var r router.Route
		c.Assert(json.Unmarshal(e.Data, &r), IsNil)
		c.Assert(r.ID, Equals, route.ID)
		c.Assert(r.Type, Equals, "http")
		c.Assert(r.Domain, Equals, route.Domain)
		c.Assert(r.Service, Equals, route.Service)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for create event")
	}

	c.Assert(s.c.DeleteRoute(app.ID, route.FormattedID()), IsNil)
	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		c.Assert(e.ObjectType, Equals, ct.EventTypeRouteDeletion)
		var r router.Route
		c.Assert(json.Unmarshal(e.Data, &r), IsNil)
		c.Assert(r.ID, Equals, route.ID)
		c.Assert(r.Type, Equals, "http")
		c.Assert(r.Domain, Equals, route.Domain)
		c.Assert(r.Service, Equals, route.Service)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for remove event")
	}

	route = s.createTestRoute(c, app.ID, (&router.TCPRoute{Service: "bar"}).ToRoute())
	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		c.Assert(e.ObjectType, Equals, ct.EventTypeRoute)
		var r router.Route
		c.Assert(json.Unmarshal(e.Data, &r), IsNil)
		c.Assert(r.ID, Equals, route.ID)
		c.Assert(r.Type, Equals, "tcp")
		c.Assert(r.Service, Equals, route.Service)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for create event")
	}

	c.Assert(s.c.DeleteRoute(app.ID, route.FormattedID()), IsNil)
	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		c.Assert(e.ObjectType, Equals, ct.EventTypeRouteDeletion)
		var r router.Route
		c.Assert(json.Unmarshal(e.Data, &r), IsNil)
		c.Assert(r.ID, Equals, route.ID)
		c.Assert(r.Type, Equals, "tcp")
		c.Assert(r.Service, Equals, route.Service)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for remove event")
	}
}
