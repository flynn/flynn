package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/flynn/strowger/types"
	. "github.com/titanous/gocheck"
)

func newTestAPIServer() *testAPIServer {
	etcd := newFakeEtcd()
	httpListener, _, _ := newHTTPListener(etcd)
	tcpListener, _, _ := newTCPListener(etcd)
	r := &Router{
		HTTP: httpListener,
		TCP:  tcpListener,
	}
	return &testAPIServer{
		Server:    httptest.NewServer(apiHandler(r)),
		listeners: []Listener{r.HTTP, r.TCP},
	}
}

type testAPIServer struct {
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

func (s *testAPIServer) Get(path string, v interface{}) (*http.Response, error) {
	res, err := http.Get(s.URL + path)
	if err != nil {
		return nil, err
	}
	if v != nil {
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return res, fmt.Errorf("Unexpected status code %d", res.StatusCode)
		}
		return res, json.NewDecoder(res.Body).Decode(v)
	}
	return res, nil
}

func (s *testAPIServer) Post(path string, in, out interface{}) (*http.Response, error) {
	buf, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	res, err := http.Post(s.URL+path, "application/json", bytes.NewBuffer(buf))
	if err != nil {
		return nil, err
	}
	if out != nil {
		if res.StatusCode != 200 {
			return res, fmt.Errorf("Unexpected status code %d", res.StatusCode)
		}
		defer res.Body.Close()
		return res, json.NewDecoder(res.Body).Decode(out)
	}
	return res, nil
}

func (s *testAPIServer) Delete(path string) (*http.Response, error) {
	req, err := http.NewRequest("DELETE", s.URL+path, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

func (s *S) TestAPIAddTCPRoute(c *C) {
	srv := newTestAPIServer()
	defer srv.Close()

	r := (&strowger.TCPRoute{Service: "test"}).ToRoute()
	route := &strowger.Route{}
	_, err := srv.Post("/routes", r, route)
	c.Assert(err, IsNil)

	tcpRoute := route.TCPRoute()
	c.Assert(tcpRoute.ID, Not(Equals), "")
	c.Assert(tcpRoute.CreatedAt, Not(IsNil))
	c.Assert(tcpRoute.UpdatedAt, Not(IsNil))
	c.Assert(tcpRoute.Service, Equals, "test")
	c.Assert(tcpRoute.Port, Not(Equals), 0)

	route = &strowger.Route{}
	_, err = srv.Get(tcpRoute.ID, route)
	c.Assert(err, IsNil)

	getTCPRoute := route.TCPRoute()
	c.Assert(getTCPRoute.ID, Equals, tcpRoute.ID)
	c.Assert(getTCPRoute.CreatedAt, DeepEquals, tcpRoute.CreatedAt)
	c.Assert(getTCPRoute.UpdatedAt, DeepEquals, tcpRoute.UpdatedAt)
	c.Assert(getTCPRoute.Service, Equals, "test")
	c.Assert(getTCPRoute.Port, Equals, tcpRoute.Port)

	_, err = srv.Delete(route.ID)
	c.Assert(err, IsNil)
	res, err := srv.Get(route.ID, nil)
	c.Assert(err, IsNil)
	res.Body.Close()
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) TestAPIAddHTTPRoute(c *C) {
	srv := newTestAPIServer()
	defer srv.Close()

	r := (&strowger.HTTPRoute{Domain: "example.com", Service: "test"}).ToRoute()
	route := &strowger.Route{}
	_, err := srv.Post("/routes", r, route)
	c.Assert(err, IsNil)

	httpRoute := route.HTTPRoute()
	c.Assert(httpRoute.ID, Not(Equals), "")
	c.Assert(httpRoute.CreatedAt, Not(IsNil))
	c.Assert(httpRoute.UpdatedAt, Not(IsNil))
	c.Assert(httpRoute.Service, Equals, "test")
	c.Assert(httpRoute.Domain, Equals, "example.com")

	route = &strowger.Route{}
	_, err = srv.Get(httpRoute.ID, route)
	c.Assert(err, IsNil)

	getHTTPRoute := route.HTTPRoute()
	c.Assert(getHTTPRoute.ID, Equals, httpRoute.ID)
	c.Assert(getHTTPRoute.CreatedAt, DeepEquals, httpRoute.CreatedAt)
	c.Assert(getHTTPRoute.UpdatedAt, DeepEquals, httpRoute.UpdatedAt)
	c.Assert(getHTTPRoute.Service, Equals, "test")
	c.Assert(getHTTPRoute.Domain, Equals, "example.com")

	_, err = srv.Delete(route.ID)
	c.Assert(err, IsNil)
	res, err := srv.Get(route.ID, nil)
	c.Assert(err, IsNil)
	res.Body.Close()
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) TestAPIListRoutes(c *C) {
	srv := newTestAPIServer()
	defer srv.Close()

	r0 := (&strowger.HTTPRoute{Domain: "example.com", Service: "test"}).ToRoute()
	r1 := (&strowger.HTTPRoute{Domain: "example.net", Service: "test", Route: &strowger.Route{ParentRef: "foo"}}).ToRoute()
	r2 := (&strowger.TCPRoute{Service: "test"}).ToRoute()
	r3 := (&strowger.TCPRoute{Service: "test", Route: &strowger.Route{ParentRef: "foo"}}).ToRoute()

	_, err := srv.Post("/routes", r0, r0)
	c.Assert(err, IsNil)
	_, err = srv.Post("/routes", r1, r1)
	c.Assert(err, IsNil)
	_, err = srv.Post("/routes", r2, r2)
	c.Assert(err, IsNil)
	_, err = srv.Post("/routes", r3, r3)
	c.Assert(err, IsNil)

	var routes []*strowger.Route
	_, err = srv.Get("/routes", &routes)
	c.Assert(err, IsNil)
	c.Assert(routes, HasLen, 4)
	c.Assert(routes[3].ID, Equals, r0.ID)
	c.Assert(routes[2].ID, Equals, r1.ID)
	c.Assert(routes[1].ID, Equals, r2.ID)
	c.Assert(routes[0].ID, Equals, r3.ID)

	_, err = srv.Get("/routes?parent_ref=foo", &routes)
	c.Assert(err, IsNil)
	c.Assert(routes, HasLen, 2)
	c.Assert(routes[1].ID, Equals, r1.ID)
	c.Assert(routes[0].ID, Equals, r3.ID)
}
