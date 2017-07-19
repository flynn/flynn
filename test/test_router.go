package main

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/tlscert"
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
	watcher, err := x.controller.WatchJobEvents("router", "")
	t.Assert(err, c.IsNil)
	t.Assert(x.flynn("/", "-a", "router", "env", "set", "ADDITIONAL_HTTP_PORTS=8080", "ADDITIONAL_HTTPS_PORTS=8081"), Succeeds)
	t.Assert(watcher.WaitFor(ct.JobEvents{"app": {ct.JobStateUp: 1, ct.JobStateDown: 1}}, 10*time.Second, nil), c.IsNil)

	// check a non-routed HTTP request to an additional port fails
	req, err := http.NewRequest("GET", "http://dashboard."+x.Domain+":8080", nil)
	t.Assert(err, c.IsNil)
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
	res, err = http.DefaultClient.Do(req)
	t.Assert(err, c.IsNil)
	t.Assert(res.StatusCode, c.Equals, http.StatusOK)
	res.Body.Close()

	writeTemp := func(data, prefix string) (string, error) {
		f, err := ioutil.TempFile(os.TempDir(), fmt.Sprintf("flynn-test-%s", prefix))
		t.Assert(err, c.IsNil)
		_, err = f.WriteString(data)
		t.Assert(err, c.IsNil)
		stat, err := f.Stat()
		t.Assert(err, c.IsNil)
		return filepath.Join(os.TempDir(), stat.Name()), nil
	}

	// add an HTTPS controller route on the new port
	cert, err := tlscert.Generate([]string{"dashboard." + x.Domain})
	t.Assert(err, c.IsNil)
	certPath, err := writeTemp(cert.Cert, "tls-cert")
	t.Assert(err, c.IsNil)
	keyPath, err := writeTemp(cert.PrivateKey, "tls-key")
	certRoute := x.flynn("/", "-a", "dashboard", "route", "add", "http", "-s", "dashboard-web", "-p", "8081", "--tls-cert", certPath, "--tls-key", keyPath, "dashboard."+x.Domain)
	t.Assert(certRoute, Succeeds)

	// pause to allow router to catch up (see above)
	time.Sleep(1 * time.Second)

	// ignore TLS CA issues
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}

	// check a routed HTTP request succeeds
	req, err = http.NewRequest("GET", "https://dashboard."+x.Domain+":8081", nil)
	t.Assert(err, c.IsNil)
	res, err = client.Do(req)
	t.Assert(err, c.IsNil)
	t.Assert(res.StatusCode, c.Equals, http.StatusOK)
	res.Body.Close()

	// check that a HTTPS request to the default port succeeds
	req, err = http.NewRequest("GET", "https://dashboard."+x.Domain, nil)
	t.Assert(err, c.IsNil)
	res, err = client.Do(req)
	t.Assert(err, c.IsNil)
	t.Assert(res.StatusCode, c.Equals, http.StatusOK)
	res.Body.Close()
}
