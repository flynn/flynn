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

	s.discoverd.Register("test", srv1.Listener.Addr().String())
	defer s.discoverd.UnregisterAll()

	fe, err := s.newHTTPFrontend()
	c.Assert(err, IsNil)
	defer fe.Close()

	err = fe.AddHTTPDomain("example.com", "test", nil, nil)
	c.Assert(err, IsNil)

	req, err := http.NewRequest("GET", "http://"+fe.Addr, nil)
	c.Assert(err, IsNil)
	req.Host = "example.com"
	res, err := http.DefaultClient.Do(req)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	data, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "1")

	s.discoverd.Unregister("test", srv1.Listener.Addr().String())
	s.discoverd.Register("test", srv2.Listener.Addr().String())

	// Close the connection we just used to trigger a new backend choice
	http.DefaultTransport.(*http.Transport).CloseIdleConnections()

	res, err = http.DefaultClient.Do(req)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	data, err = ioutil.ReadAll(res.Body)
	res.Body.Close()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "2")

	err = fe.RemoveHTTPDomain("example.com")
	c.Assert(err, IsNil)
	http.DefaultTransport.(*http.Transport).CloseIdleConnections()

	res, err = http.DefaultClient.Do(req)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 404)
	res.Body.Close()
}
