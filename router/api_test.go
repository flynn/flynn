package main

import (
	"net/http/httptest"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/testutil/etcdrunner"
	"github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
)

func (s *S) newTestAPIServer(t etcdrunner.TestingT) *testAPIServer {
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

	route, err := srv.GetRoute("tcp", tcpRoute.ID)
	c.Assert(err, IsNil)

	getTCPRoute := route.TCPRoute()
	c.Assert(getTCPRoute.ID, Equals, tcpRoute.ID)
	c.Assert(getTCPRoute.CreatedAt, DeepEquals, tcpRoute.CreatedAt)
	c.Assert(getTCPRoute.UpdatedAt, DeepEquals, tcpRoute.UpdatedAt)
	c.Assert(getTCPRoute.Service, Equals, "test")
	c.Assert(getTCPRoute.Port, Equals, tcpRoute.Port)

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

	route, err := srv.GetRoute("http", httpRoute.ID)
	c.Assert(err, IsNil)
	c.Assert(httpRoute.ID, Not(Equals), "")

	getHTTPRoute := route.HTTPRoute()
	c.Assert(getHTTPRoute.ID, Equals, httpRoute.ID)
	c.Assert(getHTTPRoute.CreatedAt, DeepEquals, httpRoute.CreatedAt)
	c.Assert(getHTTPRoute.UpdatedAt, DeepEquals, httpRoute.UpdatedAt)
	c.Assert(getHTTPRoute.Service, Equals, "test")
	c.Assert(getHTTPRoute.Domain, Equals, "example.com")

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

	r = router.HTTPRoute{ID: r.ID, Domain: "example.com", Service: "bar"}.ToRoute()
	err = srv.UpdateRoute(r)
	c.Assert(err, IsNil)
}

func (s *S) TestAPIListRoutes(c *C) {
	srv := s.newTestAPIServer(c)
	defer srv.Close()

	r0 := router.HTTPRoute{Domain: "example.com", Service: "test"}.ToRoute()
	r1 := router.HTTPRoute{Domain: "example.net", Service: "test", ParentRef: "foo"}.ToRoute()
	r2 := router.TCPRoute{Service: "test"}.ToRoute()
	r3 := router.TCPRoute{Service: "test", ParentRef: "foo"}.ToRoute()

	err := srv.CreateRoute(r0)
	c.Assert(err, IsNil)
	err = srv.CreateRoute(r1)
	c.Assert(err, IsNil)
	err = srv.CreateRoute(r2)
	c.Assert(err, IsNil)
	err = srv.CreateRoute(r3)
	c.Assert(err, IsNil)

	routes, err := srv.ListRoutes("")
	c.Assert(err, IsNil)
	c.Assert(routes, HasLen, 4)
	c.Assert(routes[3].ID, Equals, r0.ID)
	c.Assert(routes[2].ID, Equals, r1.ID)
	c.Assert(routes[1].ID, Equals, r2.ID)
	c.Assert(routes[0].ID, Equals, r3.ID)

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
	stream, err := client.StreamEvents(events)
	c.Assert(err, IsNil)
	defer stream.Close()

	r := addHTTPRoute(c, l)
	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		c.Assert(e.Event, Equals, "set")
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
		c.Assert(e.Event, Equals, "remove")
		c.Assert(e.Route.ID, Equals, r.ID)
		c.Assert(e.Route.Type, Equals, "http")
		c.Assert(e.Error, IsNil)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for remove event")
	}

	tcpr := addTCPRoute(c, tcpl, 46000)
	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		c.Assert(e.Event, Equals, "set")
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
		c.Assert(e.Event, Equals, "remove")
		c.Assert(e.Route.ID, Equals, tcpr.ID)
		c.Assert(e.Route.Type, Equals, "tcp")
		c.Assert(e.Error, IsNil)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for remove event")
	}
}
