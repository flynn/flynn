package main

import (
	"net/http/httptest"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/testutil/etcdrunner"
	"github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
)

func newTestAPIServer(t etcdrunner.TestingT) *testAPIServer {
	ec, etcdAddr, killEtcd := newEtcd(t)
	dc, killDiscoverd := newDiscoverd(t, etcdAddr)

	httpListener, _ := newHTTPListenerClients(t, ec, dc)
	tcpListener, _ := newTCPListenerClients(t, ec, dc)
	r := &Router{
		HTTP: httpListener,
		TCP:  tcpListener,
	}
	ts := &testAPIServer{
		Server:    httptest.NewServer(apiHandler(r)),
		listeners: []Listener{r.HTTP, r.TCP},
		cleanup:   []func(){killDiscoverd, killEtcd},
	}

	ts.Client = client.NewWithAddr(ts.Listener.Addr().String())
	return ts
}

type testAPIServer struct {
	client.Client
	*httptest.Server
	listeners []Listener
	cleanup   []func()
}

func (s *testAPIServer) Close() error {
	s.Server.Close()
	for _, l := range s.listeners {
		l.Close()
	}
	for _, cleanup := range s.cleanup {
		cleanup()
	}
	s.Client.Close()
	return nil
}

func (s *S) TestAPIAddTCPRoute(c *C) {
	srv := newTestAPIServer(c)
	defer srv.Close()

	r := (&router.TCPRoute{Service: "test"}).ToRoute()
	err := srv.CreateRoute(r)
	c.Assert(err, IsNil)

	tcpRoute := r.TCPRoute()
	c.Assert(tcpRoute.ID, Not(Equals), "")
	c.Assert(tcpRoute.CreatedAt, Not(IsNil))
	c.Assert(tcpRoute.UpdatedAt, Not(IsNil))
	c.Assert(tcpRoute.Service, Equals, "test")
	c.Assert(tcpRoute.Port, Not(Equals), 0)

	route, err := srv.GetRoute(tcpRoute.ID)
	c.Assert(err, IsNil)

	getTCPRoute := route.TCPRoute()
	c.Assert(getTCPRoute.ID, Equals, tcpRoute.ID)
	c.Assert(getTCPRoute.CreatedAt, DeepEquals, tcpRoute.CreatedAt)
	c.Assert(getTCPRoute.UpdatedAt, DeepEquals, tcpRoute.UpdatedAt)
	c.Assert(getTCPRoute.Service, Equals, "test")
	c.Assert(getTCPRoute.Port, Equals, tcpRoute.Port)

	err = srv.DeleteRoute(route.ID)
	c.Assert(err, IsNil)
	_, err = srv.GetRoute(route.ID)
	c.Assert(err, Equals, client.ErrNotFound)
}

func (s *S) TestAPIAddHTTPRoute(c *C) {
	srv := newTestAPIServer(c)
	defer srv.Close()

	r := (&router.HTTPRoute{Domain: "example.com", Service: "test"}).ToRoute()
	err := srv.CreateRoute(r)
	c.Assert(err, IsNil)

	httpRoute := r.HTTPRoute()
	c.Assert(httpRoute.ID, Not(Equals), "")
	c.Assert(httpRoute.CreatedAt, Not(IsNil))
	c.Assert(httpRoute.UpdatedAt, Not(IsNil))
	c.Assert(httpRoute.Service, Equals, "test")
	c.Assert(httpRoute.Domain, Equals, "example.com")

	route, err := srv.GetRoute(httpRoute.ID)
	c.Assert(err, IsNil)

	getHTTPRoute := route.HTTPRoute()
	c.Assert(getHTTPRoute.ID, Equals, httpRoute.ID)
	c.Assert(getHTTPRoute.CreatedAt, DeepEquals, httpRoute.CreatedAt)
	c.Assert(getHTTPRoute.UpdatedAt, DeepEquals, httpRoute.UpdatedAt)
	c.Assert(getHTTPRoute.Service, Equals, "test")
	c.Assert(getHTTPRoute.Domain, Equals, "example.com")

	err = srv.DeleteRoute(route.ID)
	c.Assert(err, IsNil)
	_, err = srv.GetRoute(route.ID)
	c.Assert(err, Equals, client.ErrNotFound)
}

func (s *S) TestAPISetHTTPRoute(c *C) {
	srv := newTestAPIServer(c)
	defer srv.Close()

	r := (&router.HTTPRoute{Domain: "example.com", Service: "foo"}).ToRoute()
	err := srv.SetRoute(r)
	c.Assert(err, IsNil)

	r = (&router.HTTPRoute{Domain: "example.com", Service: "bar"}).ToRoute()
	err = srv.SetRoute(r)
	c.Assert(err, IsNil)
}

func (s *S) TestAPIListRoutes(c *C) {
	srv := newTestAPIServer(c)
	defer srv.Close()

	r0 := (&router.HTTPRoute{Domain: "example.com", Service: "test"}).ToRoute()
	r1 := (&router.HTTPRoute{Domain: "example.net", Service: "test", Route: &router.Route{ParentRef: "foo"}}).ToRoute()
	r2 := (&router.TCPRoute{Service: "test"}).ToRoute()
	r3 := (&router.TCPRoute{Service: "test", Route: &router.Route{ParentRef: "foo"}}).ToRoute()

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
