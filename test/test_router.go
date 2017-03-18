package main

import (
	"net/http"
	"time"

	c "github.com/flynn/go-check"
)

type RouterSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&RouterSuite{})

func (s *RouterSuite) TestAdditionalHttpPorts(t *c.C) {
	// boot 1 node cluster
	x := s.bootCluster(t, 1)
	defer x.Destroy()

	// Test that setting added HTTP and HTTPS ports succeeds
	t.Assert(x.flynn("/", "-a", "router", "env", "set", "ADDITIONAL_HTTP_PORTS=8080"), Succeeds)
	t.Assert(x.flynn("/", "-a", "router", "env", "set", "ADDITIONAL_HTTPS_PORTS=8081"), Succeeds)

	// check a non-routed HTTP request to an additional port fails
	req, err := http.NewRequest("GET", "http://dashboard."+x.Domain+":8080", nil)
	t.Assert(err, c.IsNil)
	req.SetBasicAuth("", x.Key)
	res, err := http.DefaultClient.Do(req)
	t.Assert(err, c.IsNil)
	t.Assert(res.StatusCode, c.Equals, http.StatusNotFound)
	res.Body.Close()

	// add a controller route on the new port
	t.Assert(x.flynn("/", "-a", "dashboard", "route", "add", "http", "-s", "dashboard-web", "-p", "8080", "dashboard."+x.Domain), Succeeds)

	// The router API does not currently give us a synchronous result on
	// "route add", so we must pause for a moment to let it catch up. This seems
	// related to the issue mentioned in CLISuite.TestRoute().
	time.Sleep(1 * time.Second)

	// check a routed HTTP request succeeds
	req, err = http.NewRequest("GET", "http://dashboard."+x.Domain+":8080", nil)
	t.Assert(err, c.IsNil)
	req.SetBasicAuth("", x.Key)
	res, err = http.DefaultClient.Do(req)
	t.Assert(err, c.IsNil)
	t.Assert(res.StatusCode, c.Equals, http.StatusOK)
	res.Body.Close()

	// check that a HTTP request to the default port succeeds
	req, err = http.NewRequest("GET", "http://dashboard."+x.Domain, nil)
	t.Assert(err, c.IsNil)
	req.SetBasicAuth("", x.Key)
	res, err = http.DefaultClient.Do(req)
	t.Assert(err, c.IsNil)
	t.Assert(res.StatusCode, c.Equals, http.StatusOK)
	res.Body.Close()
}
