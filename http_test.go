package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	. "github.com/titanous/gocheck"
)

func (s *S) TestAddHTTPDomain(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l, discoverd, err := newHTTPListener(nil)
	c.Assert(err, IsNil)
	defer l.Close()

	discoverd.Register("test", srv1.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	err = l.AddHTTPDomain("example.com", "test", nil, nil)
	c.Assert(err, IsNil)

	assertGet(c, l.Addr, "/", "example.com", "1")

	discoverd.Unregister("test", srv1.Listener.Addr().String())
	discoverd.Register("test", srv2.Listener.Addr().String())

	// Close the connection we just used to trigger a new backend choice
	http.DefaultTransport.(*http.Transport).CloseIdleConnections()

	assertGet(c, l.Addr, "/", "example.com", "2")

	err = l.RemoveHTTPDomain("example.com")
	c.Assert(err, IsNil)
	http.DefaultTransport.(*http.Transport).CloseIdleConnections()

	res, err := http.DefaultClient.Do(newReq(l.Addr, "/", "example.com"))
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 404)
	res.Body.Close()
}

func newReq(addr, path, host string) *http.Request {
	req, _ := http.NewRequest("GET", "http://"+addr+path, nil)
	req.Host = host
	return req
}

func assertGet(c *C, addr, path, host, expected string) {
	res, err := http.DefaultClient.Do(newReq(addr, path, host))
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	data, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, expected)
}

func (s *S) TestInitialSync(c *C) {
	etcd := newFakeEtcd()
	l, _, err := newHTTPListener(etcd)
	c.Assert(err, IsNil)
	err = l.AddHTTPDomain("example.com", "test", nil, nil)
	c.Assert(err, IsNil)
	l.Close()

	srv := httptest.NewServer(httpTestHandler("1"))
	defer srv.Close()

	l, discoverd, err := newHTTPListener(etcd)
	c.Assert(err, IsNil)
	defer l.Close()

	discoverd.Register("test", srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	assertGet(c, l.Addr, "/", "example.com", "1")
}
