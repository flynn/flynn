package main

import (
	"net/http/httptest"
	"strings"
	"time"

	"github.com/flynn/flynn/discoverd/testutil"
	"github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
	. "github.com/flynn/go-check"
)

func (s *S) newTestAPIServer(t testutil.TestingT) *testAPIServer {
	httpListener := s.newHTTPListener(t)
	tcpListener := s.newTCPListener(t)
	r := &Router{
		HTTP: httpListener,
		TCP:  tcpListener,
	}
	ts := &testAPIServer{
		Server:    httptest.NewServer(apiHandler(r)),
		listeners: []Listener{r.HTTP, r.TCP},
	}

	ts.Client = client.NewWithAddr(ts.Listener.Addr().String())
	return ts
}

type testAPIServer struct {
	client.Client
	*httptest.Server
	listeners []Listener
}

func (s *testAPIServer) Close() error {
	s.Server.Close()
	for _, l := range s.listeners {
		l.Close()
	}
	return nil
}

func (s *S) TestAPIAddTCPRoute(c *C) {
	srv := s.newTestAPIServer(c)
	defer srv.Close()

	r := router.TCPRoute{Service: "test"}.ToRoute()
	err := srv.CreateRoute(r)
	c.Assert(err, IsNil)

	tcpRoute := r.TCPRoute()
	c.Assert(tcpRoute.ID, Not(Equals), "")
	c.Assert(tcpRoute.CreatedAt, Not(IsNil))
	c.Assert(tcpRoute.UpdatedAt, Not(IsNil))
	c.Assert(tcpRoute.Service, Equals, "test")
	c.Assert(tcpRoute.Port, Not(Equals), 0)
	c.Assert(tcpRoute.Leader, Equals, false)

	route, err := srv.GetRoute("tcp", tcpRoute.ID)
	c.Assert(err, IsNil)

	getTCPRoute := route.TCPRoute()
	c.Assert(getTCPRoute.ID, Equals, tcpRoute.ID)
	c.Assert(getTCPRoute.CreatedAt, DeepEquals, tcpRoute.CreatedAt)
	c.Assert(getTCPRoute.UpdatedAt, DeepEquals, tcpRoute.UpdatedAt)
	c.Assert(getTCPRoute.Service, Equals, "test")
	c.Assert(getTCPRoute.Port, Equals, tcpRoute.Port)
	c.Assert(getTCPRoute.Leader, Equals, false)

	err = srv.DeleteRoute("tcp", route.ID)
	c.Assert(err, IsNil)
	_, err = srv.GetRoute("tcp", route.ID)
	c.Assert(err, Equals, client.ErrNotFound)
}

func (s *S) TestAPIAddHTTPRoute(c *C) {
	srv := s.newTestAPIServer(c)
	defer srv.Close()

	r := router.HTTPRoute{Domain: "example.com", Service: "test"}.ToRoute()
	err := srv.CreateRoute(r)
	c.Assert(err, IsNil)

	httpRoute := r.HTTPRoute()
	c.Assert(httpRoute.ID, Not(Equals), "")
	c.Assert(httpRoute.CreatedAt, Not(IsNil))
	c.Assert(httpRoute.UpdatedAt, Not(IsNil))
	c.Assert(httpRoute.Service, Equals, "test")
	c.Assert(httpRoute.Domain, Equals, "example.com")
	c.Assert(httpRoute.Sticky, Equals, false)
	c.Assert(httpRoute.Leader, Equals, false)

	route, err := srv.GetRoute("http", httpRoute.ID)
	c.Assert(err, IsNil)
	c.Assert(httpRoute.ID, Not(Equals), "")

	getHTTPRoute := route.HTTPRoute()
	c.Assert(getHTTPRoute.ID, Equals, httpRoute.ID)
	c.Assert(getHTTPRoute.CreatedAt, DeepEquals, httpRoute.CreatedAt)
	c.Assert(getHTTPRoute.UpdatedAt, DeepEquals, httpRoute.UpdatedAt)
	c.Assert(getHTTPRoute.Service, Equals, "test")
	c.Assert(getHTTPRoute.Domain, Equals, "example.com")
	c.Assert(getHTTPRoute.Sticky, Equals, false)
	c.Assert(getHTTPRoute.Leader, Equals, false)

	err = srv.DeleteRoute("http", route.ID)
	c.Assert(err, IsNil)
	_, err = srv.GetRoute("http", route.ID)
	c.Assert(err, Equals, client.ErrNotFound)
}

func (s *S) TestAPIAddDuplicateRoute(c *C) {
	srv := s.newTestAPIServer(c)
	defer srv.Close()

	// first create route
	r := router.HTTPRoute{Domain: "example.com", Service: "test"}.ToRoute()
	err := srv.CreateRoute(r)
	c.Assert(err, IsNil)

	// ensure we got back what we expect
	route := r.HTTPRoute()
	c.Assert(route.ID, Not(Equals), "")
	c.Assert(route.CreatedAt, Not(IsNil))
	c.Assert(route.UpdatedAt, Not(IsNil))
	c.Assert(route.Service, Equals, "test")
	c.Assert(route.Domain, Equals, "example.com")

	// attempt to create the same route again, ensure fails with conflict
	err = srv.CreateRoute(r)
	c.Assert(err, Not(IsNil))
	c.Assert(err.Error(), Equals, "conflict: Duplicate route")

	// delete the route
	err = srv.DeleteRoute("http", route.ID)
	c.Assert(err, IsNil)
	_, err = srv.GetRoute("http", route.ID)
	c.Assert(err, Equals, client.ErrNotFound)
}

func (s *S) TestAPISetHTTPRoute(c *C) {
	srv := s.newTestAPIServer(c)
	defer srv.Close()

	r := router.HTTPRoute{Domain: "example.com", Service: "foo"}.ToRoute()
	err := srv.CreateRoute(r)
	c.Assert(err, IsNil)
	c.Assert(r.ID, Not(IsNil))

	r = router.HTTPRoute{ID: r.ID, Domain: "example.com", Service: "bar", Leader: true, Sticky: true}.ToRoute()
	err = srv.UpdateRoute(r)
	c.Assert(err, IsNil)
	r, err = srv.GetRoute("http", r.ID)
	c.Assert(err, IsNil)
	c.Assert(r.Sticky, Equals, true)
	c.Assert(r.Leader, Equals, true)
	c.Assert(r.Service, Equals, "bar")
}

func (s *S) TestAPISetTCPRoute(c *C) {
	srv := s.newTestAPIServer(c)
	defer srv.Close()

	r := router.TCPRoute{Service: "foo"}.ToRoute()
	err := srv.CreateRoute(r)
	c.Assert(err, IsNil)
	c.Assert(r.ID, Not(IsNil))

	r = router.TCPRoute{ID: r.ID, Service: "bar", Port: int(r.Port), Leader: true}.ToRoute()
	err = srv.UpdateRoute(r)
	c.Assert(err, IsNil)
	r, err = srv.GetRoute("tcp", r.ID)
	c.Assert(err, IsNil)
	c.Assert(r.Leader, Equals, true)
	c.Assert(r.Service, Equals, "bar")
}

func (s *S) TestAPIListRoutes(c *C) {
	srv := s.newTestAPIServer(c)
	defer srv.Close()

	r0 := router.HTTPRoute{Domain: "example.com", Service: "test"}.ToRoute()
	r1 := router.HTTPRoute{Domain: "example.net", Service: "test", ParentRef: "foo"}.ToRoute()
	r2 := router.TCPRoute{Service: "test"}.ToRoute()
	r3 := router.TCPRoute{Service: "test", ParentRef: "foo"}.ToRoute()

	tlsCert := tlsConfigForDomain("*.bar.example.org")
	r4 := router.HTTPRoute{
		Domain:  "1.bar.example.org",
		Service: "test",
		Certificate: &router.Certificate{
			Cert: tlsCert.Cert,
			Key:  tlsCert.PrivateKey,
		},
	}.ToRoute()

	err := srv.CreateRoute(r0)
	c.Assert(err, IsNil)
	err = srv.CreateRoute(r1)
	c.Assert(err, IsNil)
	err = srv.CreateRoute(r2)
	c.Assert(err, IsNil)
	err = srv.CreateRoute(r3)
	c.Assert(err, IsNil)
	err = srv.CreateRoute(r4)
	c.Assert(err, IsNil)

	routes, err := srv.ListRoutes("")
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

	routes, err = srv.ListRoutes("foo")
	c.Assert(err, IsNil)
	c.Assert(routes, HasLen, 2)
	c.Assert(routes[1].ID, Equals, r1.ID)
	c.Assert(routes[0].ID, Equals, r3.ID)
}

func (s *S) TestStreamEvents(c *C) {
	srv := s.newTestAPIServer(c)
	client := srv.Client
	defer srv.Close()

	l := srv.listeners[0].(*HTTPListener)
	tcpl := srv.listeners[1].(*TCPListener)

	events := make(chan *router.StreamEvent)
	stream, err := client.StreamEvents(nil, events)
	c.Assert(err, IsNil)
	defer stream.Close()

	r := addHTTPRoute(c, l)
	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		c.Assert(e.Event, Equals, router.EventTypeRouteSet)
		c.Assert(e.Route.ID, Equals, r.ID)
		c.Assert(e.Route.Type, Equals, "http")
		c.Assert(e.Error, IsNil)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for set event")
	}

	removeRoute(c, l, r.ID)
	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		c.Assert(e.Event, Equals, router.EventTypeRouteRemove)
		c.Assert(e.Route.ID, Equals, r.ID)
		c.Assert(e.Route.Type, Equals, "http")
		c.Assert(e.Error, IsNil)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for remove event")
	}

	tcpr := addTCPRoute(c, tcpl, allocatePort())
	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		c.Assert(e.Event, Equals, router.EventTypeRouteSet)
		c.Assert(e.Route.ID, Equals, tcpr.ID)
		c.Assert(e.Route.Type, Equals, "tcp")
		c.Assert(e.Error, IsNil)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for set event")
	}

	removeRoute(c, tcpl, tcpr.ID)
	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		c.Assert(e.Event, Equals, router.EventTypeRouteRemove)
		c.Assert(e.Route.ID, Equals, tcpr.ID)
		c.Assert(e.Route.Type, Equals, "tcp")
		c.Assert(e.Error, IsNil)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for remove event")
	}
}
