package main

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/api"
	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/tlscert"
	"github.com/flynn/flynn/router/testutils"
	router "github.com/flynn/flynn/router/types"
	. "github.com/flynn/go-check"
	"golang.org/x/net/context"
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
	c.Assert(err.Error(), Equals, "conflict: a http route with domain=dup.example.com and path=/ already exists")
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

func (s *GRPCSuite) TestSetRoutes(c *C) {
	// create some apps
	app1 := s.createTestApp(c, &api.App{DisplayName: "app1"})
	app2 := s.createTestApp(c, &api.App{DisplayName: "app2"})
	app3 := s.createTestApp(c, &api.App{DisplayName: "app3"})
	var app1Routes, app2Routes, app3Routes []*api.Route

	// define some convenience functions
	httpRoute := func(service, domain string) *api.Route {
		return &api.Route{
			ServiceTarget: &api.Route_ServiceTarget{
				ServiceName:   service,
				DrainBackends: true,
			},
			Config: &api.Route_Http{Http: &api.Route_HTTP{
				Domain: domain,
				Path:   "/",
			}},
		}
	}
	setRoutes := func(dryRun bool, expectedState []byte) (*api.SetRoutesResponse, error) {
		req := &api.SetRoutesRequest{
			AppRoutes: []*api.AppRoutes{
				{App: app1.Name, Routes: app1Routes},
				{App: app2.Name, Routes: app2Routes},
				{App: app3.Name, Routes: app3Routes},
			},
			DryRun:        dryRun,
			ExpectedState: expectedState,
		}
		return s.routerClient.SetRoutes(context.Background(), req)
	}
	dryRun := func() *api.SetRoutesResponse {
		res, err := setRoutes(true, nil)
		c.Assert(err, IsNil)
		c.Assert(res.DryRun, Equals, true)
		c.Assert(res.AppliedToState, Not(IsNil))
		return res
	}
	applyRoutes := func(expectedState []byte) *api.SetRoutesResponse {
		res, err := setRoutes(false, expectedState)
		c.Assert(err, IsNil)
		c.Assert(res.DryRun, Equals, false)
		c.Assert(res.AppliedToState, Not(IsNil))
		return res
	}
	assertChange := func(change *api.RouteChange, action api.RouteChange_Action, before, after *api.Route) {
		c.Assert(change.Action, Equals, action)

		if before == nil {
			c.Assert(change.Before, IsNil)
		} else {
			c.Assert(change.Before, Not(IsNil))
			c.Assert(change.Before.String(), Equals, before.String())
		}

		if after == nil {
			c.Assert(change.After, IsNil)
		} else {
			c.Assert(change.After, Not(IsNil))
			c.Assert(change.After.String(), Equals, after.String())
		}
	}
	assertNoRoutes := func() {
		var count int64
		err := s.db.QueryRow(
			"SELECT COUNT(*) FROM http_routes WHERE parent_ref IN ($1, $2, $3) AND deleted_at IS NULL",
			"controller/"+app1.Name, "controller/"+app2.Name, "controller/"+app3.Name,
		).Scan(&count)
		c.Assert(err, IsNil)
		if count != 0 {
			c.Fatalf("expected no routes, got %d", count)
		}
	}
	assertRoutes := func(expected ...*api.Route) {
		rows, err := s.db.Query(
			"SELECT service, domain FROM http_routes WHERE parent_ref IN ($1, $2, $3) AND deleted_at IS NULL ORDER BY domain",
			"controller/"+app1.Name, "controller/"+app2.Name, "controller/"+app3.Name,
		)
		c.Assert(err, IsNil)
		defer rows.Close()
		var routes []*router.Route
		for rows.Next() {
			var route router.Route
			c.Assert(rows.Scan(&route.Service, &route.Domain), IsNil)
			routes = append(routes, &route)
		}
		c.Assert(rows.Err(), IsNil)
		if len(routes) != len(expected) {
			c.Fatalf("expected %d routes, got %d", len(expected), len(routes))
		}
		for i, r := range expected {
			c.Assert(routes[i].Service, Equals, r.ServiceTarget.ServiceName)
			c.Assert(routes[i].Domain, Equals, r.Config.(*api.Route_Http).Http.Domain)
		}
	}

	// start with a route per app
	route1 := httpRoute("app1-web", "app1.example.com")
	route2 := httpRoute("app2-web", "app2.example.com")
	route3 := httpRoute("app3-web", "app3.example.com")
	app1Routes = []*api.Route{route1}
	app2Routes = []*api.Route{route2}
	app3Routes = []*api.Route{route3}

	// initial dry run should return three create changes but not create the routes
	res := dryRun()
	c.Assert(res.RouteChanges, HasLen, 3)
	assertChange(res.RouteChanges[0], api.RouteChange_ACTION_CREATE, nil, route1)
	assertChange(res.RouteChanges[1], api.RouteChange_ACTION_CREATE, nil, route2)
	assertChange(res.RouteChanges[2], api.RouteChange_ACTION_CREATE, nil, route3)
	assertNoRoutes()

	// grab the initial state so we can check re-applying fails later
	initialState := res.AppliedToState

	// applying should create the routes
	res = applyRoutes(res.AppliedToState)
	assertRoutes(route1, route2, route3)

	// trying to apply the same change again should fail
	_, err := setRoutes(false, initialState)
	c.Assert(err, NotNil)
	c.Assert(strings.Contains(err.Error(), "the expected route state in the request does not match the current state"), Equals, true, Commentf("err = %s", err))
	assertRoutes(route1, route2, route3)

	// updating a route should lead to a single update change
	newRoute1 := &api.Route{
		ServiceTarget: &api.Route_ServiceTarget{
			ServiceName:   "app1-foo",
			DrainBackends: true,
		},
		Config: route1.Config,
	}
	app1Routes = []*api.Route{newRoute1}
	res = dryRun()
	c.Assert(res.RouteChanges, HasLen, 1)
	assertChange(res.RouteChanges[0], api.RouteChange_ACTION_UPDATE, route1, newRoute1)
	assertRoutes(route1, route2, route3)
	res = applyRoutes(res.AppliedToState)
	assertRoutes(newRoute1, route2, route3)

	// adding a duplicate route should fail, both on a dry run and when applying changes
	dupRoute := &api.Route{
		ServiceTarget: &api.Route_ServiceTarget{
			ServiceName:   "app2-web",
			DrainBackends: true,
		},
		Config: route1.Config,
	}
	app2Routes = append(app2Routes, dupRoute)
	_, err = setRoutes(true, res.AppliedToState)
	c.Assert(err, NotNil)
	c.Assert(strings.Contains(err.Error(), "conflict: a http route with domain=app1.example.com and path=/ already exists"), Equals, true, Commentf("err = %s", err))
	_, err = setRoutes(false, nil)
	c.Assert(err, NotNil)
	c.Assert(strings.Contains(err.Error(), "conflict: a http route with domain=app1.example.com and path=/ already exists"), Equals, true, Commentf("err = %s", err))
	app2Routes = app2Routes[:1]

	// adding a route should lead to a single create change
	newRoute2 := httpRoute("app2-foo", "foo.example.com")
	app2Routes = append(app2Routes, newRoute2)
	res = dryRun()
	c.Assert(res.RouteChanges, HasLen, 1)
	assertChange(res.RouteChanges[0], api.RouteChange_ACTION_CREATE, nil, newRoute2)
	assertRoutes(newRoute1, route2, route3)
	res = applyRoutes(res.AppliedToState)
	assertRoutes(newRoute1, route2, route3, newRoute2)

	// removing a route should lead to a single delete change
	app2Routes = app2Routes[:1]
	res = dryRun()
	c.Assert(res.RouteChanges, HasLen, 1)
	assertChange(res.RouteChanges[0], api.RouteChange_ACTION_DELETE, newRoute2, nil)
	assertRoutes(newRoute1, route2, route3, newRoute2)
	res = applyRoutes(res.AppliedToState)
	assertRoutes(newRoute1, route2, route3)

	// making multiple changes should return the correct changes
	newRoute3 := httpRoute("app1-bar", "bar.example.com")
	newRoute4 := &api.Route{
		ServiceTarget: &api.Route_ServiceTarget{
			ServiceName:   "app2-bar",
			DrainBackends: true,
		},
		Config: route2.Config,
	}
	app1Routes = append(app1Routes, newRoute3)
	app2Routes = []*api.Route{newRoute4}
	app3Routes = []*api.Route{}
	res = dryRun()
	c.Assert(res.RouteChanges, HasLen, 3)
	assertChange(res.RouteChanges[0], api.RouteChange_ACTION_DELETE, route3, nil)
	assertChange(res.RouteChanges[1], api.RouteChange_ACTION_UPDATE, route2, newRoute4)
	assertChange(res.RouteChanges[2], api.RouteChange_ACTION_CREATE, nil, newRoute3)
	assertRoutes(newRoute1, route2, route3)
	res = applyRoutes(res.AppliedToState)
	assertRoutes(newRoute1, newRoute4, newRoute3)

	// moving a route between apps should work
	newRoute5 := &api.Route{
		ServiceTarget: &api.Route_ServiceTarget{
			ServiceName:   "app1-web",
			DrainBackends: true,
		},
		Config: route2.Config,
	}
	app1Routes = append(app1Routes, newRoute5)
	app2Routes = []*api.Route{}
	res = dryRun()
	c.Assert(res.RouteChanges, HasLen, 2)
	assertChange(res.RouteChanges[0], api.RouteChange_ACTION_DELETE, newRoute4, nil)
	assertChange(res.RouteChanges[1], api.RouteChange_ACTION_CREATE, nil, newRoute5)
	assertRoutes(newRoute1, newRoute4, newRoute3)
	res = applyRoutes(res.AppliedToState)
	assertRoutes(newRoute1, newRoute5, newRoute3)

	// setting all app routes to empty should delete all routes
	app1Routes = []*api.Route{}
	app2Routes = []*api.Route{}
	app3Routes = []*api.Route{}
	res = dryRun()
	c.Assert(res.RouteChanges, HasLen, 3)
	assertChange(res.RouteChanges[0], api.RouteChange_ACTION_DELETE, newRoute1, nil)
	assertChange(res.RouteChanges[1], api.RouteChange_ACTION_DELETE, newRoute5, nil)
	assertChange(res.RouteChanges[2], api.RouteChange_ACTION_DELETE, newRoute3, nil)
	assertRoutes(newRoute1, newRoute5, newRoute3)
	res = applyRoutes(res.AppliedToState)
	assertNoRoutes()
}
