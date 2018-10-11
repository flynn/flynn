package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil"
	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/tlscert"
	"github.com/flynn/flynn/router/schema"
	"github.com/flynn/flynn/router/types"
	. "github.com/flynn/go-check"
	"github.com/jackc/pgx"
	"golang.org/x/net/http2"
	"golang.org/x/net/websocket"
)

const UUIDRegex = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"

var httpClient = newHTTPClient("example.com")

var tlsCerts = map[string]*tlscert.Cert{
	"example.com": {
		// borrowed from net/http/httptest/server.go
		// PEM-encoded TLS cert with SAN IPs
		// "127.0.0.1" and "[::1]", expiring at the last second of 2049 (the end
		// of ASN.1 time).
		// generated from src/pkg/crypto/tls:
		// go run generate_cert.go  --rsa-bits 512 --host 127.0.0.1,::1,example.com,*.example.com --ca --start-date "Jan 1 00:00:00 1970" --duration=1000000h
		Cert: `-----BEGIN CERTIFICATE-----
MIIBmjCCAUagAwIBAgIRAP5DRqWA/pgvAnbC6gnl82kwCwYJKoZIhvcNAQELMBIx
EDAOBgNVBAoTB0FjbWUgQ28wIBcNNzAwMTAxMDAwMDAwWhgPMjA4NDAxMjkxNjAw
MDBaMBIxEDAOBgNVBAoTB0FjbWUgQ28wXDANBgkqhkiG9w0BAQEFAANLADBIAkEA
t9JXJg6fCMxvBKfLCukH7dnF1nIdCBuurjXxVM69E2+97G3aDBTIm7rXtxilAYib
BwzBtgqPzUVngbmK25cguQIDAQABo3cwdTAOBgNVHQ8BAf8EBAMCAKQwEwYDVR0l
BAwwCgYIKwYBBQUHAwEwDwYDVR0TAQH/BAUwAwEB/zA9BgNVHREENjA0ggtleGFt
cGxlLmNvbYINKi5leGFtcGxlLmNvbYcEfwAAAYcQAAAAAAAAAAAAAAAAAAAAATAL
BgkqhkiG9w0BAQsDQQBJxy1zotHYLZpyoockAlJWRa88hs1PrroUNMlueRtzNkpx
9heaebvotwUkFlnNYJZsfPnO23R0lUlzLJ3p1RNz
-----END CERTIFICATE-----`,
		PrivateKey: `-----BEGIN RSA PRIVATE KEY-----
MIIBOQIBAAJBALfSVyYOnwjMbwSnywrpB+3ZxdZyHQgbrq418VTOvRNvvext2gwU
yJu617cYpQGImwcMwbYKj81FZ4G5ituXILkCAwEAAQJAXvmhp3skdkJSFgCv6qou
O5kqG7uH/nl3DnG2iA/tJw3SlEPftQyzNk5jcIFSxvr8pu1pj+L1vw5pR68/7fre
xQIhAMM0/bYtVbzW+PPjqAev3TKhMyWkY3t9Qvw5OtgmBQ+PAiEA8RGk9OvMxBbR
8zJmOXminEE2VVE1VF0K0OiFLDG+JzcCIHurptE0B42L5E0ffeTg1hKtben7K8ug
oD+LQmyOKcahAiB05Btab2QQyQfwpsWOpP5GShCwefoj+CGgfr7kWRJdLQIgTMZe
++SKD8ascROyDnZ0Td8wbrFnO0YRPEkwlhn6h0U=
-----END RSA PRIVATE KEY-----`,
	},
}

var tlsCertsMux sync.Mutex

func normalizeDomain(domain string) string {
	domain = strings.ToLower(domain)
	if d, _, err := net.SplitHostPort(domain); err == nil {
		domain = d
	}
	uri, err := url.Parse("http://" + domain)
	if err != nil {
		panic(err)
	}
	domain = uri.Host
	if strings.HasSuffix(domain, ".example.com") {
		domain = "example.com"
	}
	return domain
}

func tlsConfigForDomain(domain string) *tlscert.Cert {
	tlsCertsMux.Lock()
	defer tlsCertsMux.Unlock()
	domain = normalizeDomain(domain)
	parts := strings.SplitAfter(domain, ".")
	wildcard := "*."
	for i := 1; i < len(parts); i++ {
		wildcard += parts[i]
	}
	if c, ok := tlsCerts[domain]; ok {
		return c
	}
	if c, ok := tlsCerts[wildcard]; ok {
		return c
	}
	c, err := tlscert.Generate([]string{domain})
	if err != nil {
		panic(err)
	}
	tlsCerts[domain] = c
	return c
}

func refreshTLSConfigForDomain(domain string) *tlscert.Cert {
	tlsCertsMux.Lock()
	defer tlsCertsMux.Unlock()
	domain = normalizeDomain(domain)
	c, err := tlscert.Generate([]string{domain})
	if err != nil {
		panic(err)
	}
	tlsCerts[domain] = c
	return c
}

func httpTestHandler(id string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(id))
	})
}

func newHTTPClient(serverName string) *http.Client {
	cert := tlsConfigForDomain(serverName)
	pool := x509.NewCertPool()
	if len(cert.CACert) > 0 {
		pool.AppendCertsFromPEM([]byte(cert.CACert))
	} else {
		pool.AppendCertsFromPEM([]byte(cert.Cert))
	}

	if strings.Contains(serverName, ":") {
		serverName, _, _ = net.SplitHostPort(serverName)
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{ServerName: serverName, RootCAs: pool},
			TLSNextProto:    map[string]func(authority string, c *tls.Conn) http.RoundTripper{}, // disable HTTP/2
		},
	}
}

func newHTTP2Client(serverName string) *http.Client {
	cert := tlsConfigForDomain(serverName)
	pool := x509.NewCertPool()
	if len(cert.CACert) > 0 {
		pool.AppendCertsFromPEM([]byte(cert.CACert))
	} else {
		pool.AppendCertsFromPEM([]byte(cert.Cert))
	}

	if strings.Contains(serverName, ":") {
		serverName, _, _ = net.SplitHostPort(serverName)
	}
	return &http.Client{
		Transport: &http2.Transport{
			TLSClientConfig: &tls.Config{ServerName: serverName, RootCAs: pool},
		},
	}
}

func (s *S) newHTTPListener(t testutil.TestingT) *HTTPListener {
	l := s.buildHTTPListener(t)
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	l.defaultPorts = getDefaultPortsFromAddrs(l)
	return l
}

func getDefaultPortsFromAddrs(l *HTTPListener) (defaultPorts []int) {
	addressArrays := [][]string{l.Addrs, l.TLSAddrs}
	for _, addressArray := range addressArrays {
		for _, addr := range addressArray {
			_, portStr, _ := net.SplitHostPort(addr)
			port, _ := strconv.Atoi(portStr)
			defaultPorts = append(defaultPorts, port)
		}
	}
	return
}

func (s *S) buildHTTPListener(t testutil.TestingT) *HTTPListener {
	cert := tlsConfigForDomain("example.com")
	pair, err := tls.X509KeyPair([]byte(cert.Cert), []byte(cert.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	l := &HTTPListener{
		Addrs:     []string{"127.0.0.1:0"},
		TLSAddrs:  []string{"127.0.0.1:0"},
		keypair:   pair,
		ds:        NewPostgresDataStore("http", s.pgx),
		discoverd: s.discoverd,
	}

	return l
}

// https://code.google.com/p/go/issues/detail?id=5381
func (s *S) TestIssue5381(c *C) {
	srv := httptest.NewServer(httpTestHandler(""))
	defer srv.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	assertGet(c, "http://"+l.Addrs[0], "example.com", "")
}

func (s *S) TestAddHTTPRoute(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	r := addHTTPRoute(c, l)

	unregister := discoverdRegisterHTTP(c, l, srv1.Listener.Addr().String())

	assertGet(c, "http://"+l.Addrs[0], "example.com", "1")
	assertGet(c, "https://"+l.TLSAddrs[0], "example.com", "1")

	unregister()
	discoverdRegisterHTTP(c, l, srv2.Listener.Addr().String())

	// Close the connection we just used to trigger a new backend choice
	httpClient.Transport.(*http.Transport).CloseIdleConnections()

	assertGet(c, "http://"+l.Addrs[0], "example.com", "2")
	assertGet(c, "https://"+l.TLSAddrs[0], "example.com", "2")

	res, err := httpClient.Do(newReq("http://"+l.Addrs[0], "example2.com"))
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 404)
	res.Body.Close()

	_, err = (&http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{ServerName: "example2.com"}},
	}).Do(newReq("https://"+l.TLSAddrs[0], "example2.com"))
	c.Assert(err, Not(IsNil))

	wait := waitForEvent(c, l, "remove", r.ID)
	err = l.RemoveRoute(r.ID)
	c.Assert(err, IsNil)
	wait()
	httpClient.Transport.(*http.Transport).CloseIdleConnections()

	res, err = httpClient.Do(newReq("http://"+l.Addrs[0], "example.com"))
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 404)
	res.Body.Close()
}

func (s *S) TestAddHTTPRouteWithCert(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	defer srv1.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	domain := "foo.example.org"
	addHTTPRouteForDomain(domain, c, l)

	unregister := discoverdRegisterHTTP(c, l, srv1.Listener.Addr().String())

	assertGet(c, "http://"+l.Addrs[0], domain, "1")
	assertGet(c, "https://"+l.TLSAddrs[0], domain, "1")

	res, err := newHTTP2Client(domain).Do(newReq("https://"+l.TLSAddrs[0], domain))
	c.Assert(err, IsNil)
	defer res.Body.Close()
	c.Assert(res.StatusCode, Equals, 200)
	data, err := ioutil.ReadAll(res.Body)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "1")

	unregister()
}

func (s *S) TestAddHTTPRouteWithInvalidCert(c *C) {
	l := s.newHTTPListener(c)
	defer l.Close()

	c1, _ := tlscert.Generate([]string{"1.example.com"})
	c2, _ := tlscert.Generate([]string{"2.example.com"})

	err := l.AddRoute(router.HTTPRoute{
		Domain:  "example.com",
		Service: "test",
		Certificate: &router.Certificate{
			Cert: c1.Cert,
			Key:  c2.PrivateKey,
		},
	}.ToRoute())
	c.Assert(err, Not(IsNil))
}

func (s *S) TestAddHTTPRouteWithExistingCert(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	tlsCert := tlsConfigForDomain("*.bar.example.org")

	domain := "1.bar.example.org"
	r1 := addRoute(c, l, router.HTTPRoute{
		Domain:  domain,
		Service: "test",
		Certificate: &router.Certificate{
			Cert: "  \n  \n " + tlsCert.Cert + "  \n",
			Key:  "\n\n" + tlsCert.PrivateKey + "\n   ",
		},
	}.ToRoute())
	unregister := discoverdRegisterHTTP(c, l, srv1.Listener.Addr().String())
	assertGet(c, "http://"+l.Addrs[0], domain, "1")
	assertGet(c, "https://"+l.TLSAddrs[0], domain, "1")
	unregister()

	domain = "2.bar.example.org"
	r2 := addHTTPRouteForDomain(domain, c, l)
	unregister = discoverdRegisterHTTP(c, l, srv2.Listener.Addr().String())
	assertGet(c, "http://"+l.Addrs[0], domain, "2")
	assertGet(c, "https://"+l.TLSAddrs[0], domain, "2")
	unregister()

	c.Assert(r1.Certificate, DeepEquals, r2.Certificate)
}

func (s *S) TestAddAndDeleteCert(c *C) {
	api := s.newTestAPIServer(c)
	defer api.Close()

	srv1 := httptest.NewServer(httpTestHandler("1"))
	defer srv1.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	domain := "chip.example.org"
	r := addHTTPRouteForDomain(domain, c, l)

	unregister := discoverdRegisterHTTP(c, l, srv1.Listener.Addr().String())
	defer unregister()

	assertGet(c, "http://"+l.Addrs[0], domain, "1")
	assertGet(c, "https://"+l.TLSAddrs[0], domain, "1")

	tlsCert := refreshTLSConfigForDomain(domain)
	cert := &router.Certificate{
		Routes: []string{r.ID},
		Cert:   tlsCert.Cert,
		Key:    tlsCert.PrivateKey,
	}
	wait := waitForEvent(c, l, "set", "")
	err := api.CreateCert(cert)
	c.Assert(err, IsNil)
	wait()

	assertGet(c, "http://"+l.Addrs[0], domain, "1")
	assertGet(c, "https://"+l.TLSAddrs[0], domain, "1")

	wait = waitForEvent(c, l, "set", "")
	err = api.DeleteCert(cert.ID)
	c.Assert(err, IsNil)
	wait()

	r, err = api.GetRoute(r.Type, r.ID)
	c.Assert(err, IsNil)
	c.Assert(r.Certificate, IsNil)
}

func (s *S) TestGetCert(c *C) {
	api := s.newTestAPIServer(c)
	defer api.Close()

	srv1 := httptest.NewServer(httpTestHandler("1"))
	defer srv1.Close()
	l := s.newHTTPListener(c)
	defer l.Close()
	domain := "oof.example.org"
	r := addHTTPRouteForDomain(domain, c, l)

	tlsCert, err := tlscert.Generate([]string{domain})
	c.Assert(err, IsNil)
	cert := &router.Certificate{
		Routes: []string{r.ID},
		Cert:   tlsCert.Cert,
		Key:    tlsCert.PrivateKey,
	}
	err = api.CreateCert(cert)
	c.Assert(err, IsNil)

	gotCert, err := api.GetCert(cert.ID)
	c.Assert(err, IsNil)
	c.Assert(gotCert.ID, Equals, cert.ID)
	c.Assert(gotCert.Cert, Equals, cert.Cert)
	c.Assert(gotCert.Key, Equals, cert.Key)
	c.Assert(gotCert.Routes, DeepEquals, cert.Routes)
}

func (s *S) TestListCerts(c *C) {
	api := s.newTestAPIServer(c)
	defer api.Close()

	srv1 := httptest.NewServer(httpTestHandler("1"))
	defer srv1.Close()
	l := s.newHTTPListener(c)
	defer l.Close()
	domain := "oof.example.org"
	r := addHTTPRouteForDomain(domain, c, l)

	tlsCert, err := tlscert.Generate([]string{domain})
	c.Assert(err, IsNil)
	cert := &router.Certificate{
		Routes: []string{r.ID},
		Cert:   tlsCert.Cert,
		Key:    tlsCert.PrivateKey,
	}
	err = api.CreateCert(cert)
	c.Assert(err, IsNil)

	gotCerts, err := api.ListCerts()
	c.Assert(err, IsNil)
	c.Assert(len(gotCerts), Equals, 2)
	gotCert := gotCerts[1] // the first cert was created with the route
	c.Assert(gotCert.ID, Equals, cert.ID)
	c.Assert(gotCert.Cert, Equals, cert.Cert)
	c.Assert(gotCert.Key, Equals, cert.Key)
	c.Assert(gotCert.Routes, DeepEquals, cert.Routes)
}

func (s *S) TestListCertRoutes(c *C) {
	api := s.newTestAPIServer(c)
	defer api.Close()

	srv1 := httptest.NewServer(httpTestHandler("1"))
	defer srv1.Close()
	l := s.newHTTPListener(c)
	defer l.Close()
	domain := "oof.example.org"
	r := addHTTPRouteForDomain(domain, c, l)

	tlsCert, err := tlscert.Generate([]string{domain})
	c.Assert(err, IsNil)
	cert := &router.Certificate{
		Routes: []string{r.ID},
		Cert:   tlsCert.Cert,
		Key:    tlsCert.PrivateKey,
	}
	err = api.CreateCert(cert)
	c.Assert(err, IsNil)

	gotRoutes, err := api.ListCertRoutes(cert.ID)
	c.Assert(err, IsNil)
	c.Assert(len(gotRoutes), Equals, 1)
	gotRoute := gotRoutes[0]
	c.Assert(gotRoute.ID, Equals, r.ID)
	c.Assert(gotRoute.Certificate, IsNil)
}

func newReq(url, host string) *http.Request {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}
	req.Host = host
	return req
}

func assertGet(c *C, url, host, expected string) []*http.Cookie {
	return assertGetCookies(c, url, host, expected, nil)
}

func assertGetCookies(c *C, url, host, expected string, cookies []*http.Cookie) []*http.Cookie {
	req := newReq(url, host)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	res, err := newHTTPClient(host).Do(req)
	c.Assert(err, IsNil)
	defer res.Body.Close()
	c.Assert(res.StatusCode, Equals, 200)
	data, err := ioutil.ReadAll(res.Body)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, expected)
	return res.Cookies()
}

func addHTTPRoute(c *C, l *HTTPListener) *router.Route {
	return addHTTPRouteForDomain("example.com", c, l)
}

func addHTTPRouteForDomain(domain string, c *C, l *HTTPListener) *router.Route {
	cert := tlsConfigForDomain(domain)
	return addRoute(c, l, router.HTTPRoute{
		Domain:  domain,
		Service: "test",
		Certificate: &router.Certificate{
			Cert: cert.Cert,
			Key:  cert.PrivateKey,
		},
	}.ToRoute())
}

func removeHTTPRoute(c *C, l *HTTPListener, id string) {
	removeRoute(c, l, id)
}

func addStickyHTTPRoute(c *C, l *HTTPListener) *router.Route {
	return addRoute(c, l, router.HTTPRoute{
		Domain:  "example.com",
		Service: "test",
		Sticky:  true,
	}.ToRoute())
}

func (s *S) TestWildcardRouting(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	srv3 := httptest.NewServer(httpTestHandler("3"))
	defer srv1.Close()
	defer srv2.Close()
	defer srv3.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, router.HTTPRoute{
		Domain:  "foo.bar",
		Service: "1",
	}.ToRoute())
	addRoute(c, l, router.HTTPRoute{
		Domain:  "*.foo.bar",
		Service: "2",
	}.ToRoute())
	addRoute(c, l, router.HTTPRoute{
		Domain:  "dev.foo.bar",
		Service: "3",
	}.ToRoute())

	discoverdRegisterHTTPService(c, l, "1", srv1.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "2", srv2.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "3", srv3.Listener.Addr().String())

	assertGet(c, "http://"+l.Addrs[0], "foo.bar", "1")
	assertGet(c, "http://"+l.Addrs[0], "flynn.foo.bar", "2")
	assertGet(c, "http://"+l.Addrs[0], "dev.foo.bar", "3")
}

func (s *S) TestWildcardCatchAllRouting(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, router.HTTPRoute{
		Domain:  "*",
		Service: "1",
	}.ToRoute())
	addRoute(c, l, router.HTTPRoute{
		Domain:  "*.bar",
		Service: "2",
	}.ToRoute())

	discoverdRegisterHTTPService(c, l, "1", srv1.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "2", srv2.Listener.Addr().String())

	assertGet(c, "http://"+l.Addrs[0], "foo", "1")
	assertGet(c, "http://"+l.Addrs[0], "example.com", "1")
	// Make sure other wildcards have priority
	assertGet(c, "http://"+l.Addrs[0], "foo.bar", "2")
}

func (s *S) TestLeaderRouting(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	client := l.discoverd
	err := client.AddService("leader-routing-http", &discoverd.ServiceConfig{
		LeaderType: discoverd.LeaderTypeManual,
	})
	c.Assert(err, IsNil)

	addRoute(c, l, router.HTTPRoute{
		Domain:  "foo.bar",
		Service: "leader-routing-http",
		Leader:  true,
	}.ToRoute())

	discoverdRegisterHTTPService(c, l, "leader-routing-http", srv1.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "leader-routing-http", srv2.Listener.Addr().String())

	discoverdSetLeaderHTTP(c, l, "leader-routing-http", md5sum("tcp-"+srv1.Listener.Addr().String()))
	assertGet(c, "http://"+l.Addrs[0], "foo.bar", "1")

	discoverdSetLeaderHTTP(c, l, "leader-routing-http", md5sum("tcp-"+srv2.Listener.Addr().String()))
	c.Assert(err, IsNil)
	assertGet(c, "http://"+l.Addrs[0], "foo.bar", "2")
}

func (s *S) TestPathRouting(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	srv3 := httptest.NewServer(httpTestHandler("3"))
	defer srv1.Close()
	defer srv2.Close()
	defer srv3.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	defRoute := addRoute(c, l, router.HTTPRoute{
		Domain:  "foo.bar",
		Service: "1",
	}.ToRoute())
	pathRoute := addRoute(c, l, router.HTTPRoute{
		Domain:  "foo.bar",
		Service: "2",
		Path:    "/2/",
	}.ToRoute())
	// test that path with no trailing slash will autocorrect
	pathRoute2 := addRoute(c, l, router.HTTPRoute{
		Domain:  "foo.bar",
		Service: "3",
		Path:    "/3",
	}.ToRoute())

	discoverdRegisterHTTPService(c, l, "1", srv1.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "2", srv2.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "3", srv3.Listener.Addr().String())

	// Check that traffic received at the path is directed to correct backend
	assertGet(c, "http://"+l.Addrs[0], "foo.bar", "1")
	assertGet(c, "http://"+l.Addrs[0]+"/2/", "foo.bar", "2")
	assertGet(c, "http://"+l.Addrs[0]+"/2", "foo.bar", "2")
	assertGet(c, "http://"+l.Addrs[0]+"/3", "foo.bar", "3")
	assertGet(c, "http://"+l.Addrs[0]+"/3/", "foo.bar", "3")

	// Test that adding a routes with invalid paths error
	invalidPaths := []string{"noleadingslash/"}
	for _, p := range invalidPaths {
		addRouteAssertErr(c, l, router.HTTPRoute{
			Domain:  "foo.bar",
			Service: "foo",
			Path:    p,
		}.ToRoute())
	}

	// Test that adding a Path route without default route fails
	addRouteAssertErr(c, l, router.HTTPRoute{
		Domain:  "foo.bar.baz",
		Service: "foo",
		Path:    "/valid/",
	}.ToRoute())

	// Test that removing the default route while there are still dependent routes fails
	removeRouteAssertErr(c, l, defRoute.ID)

	// However removing them in the appropriate order should succeed
	removeRoute(c, l, pathRoute.ID)
	removeRoute(c, l, pathRoute2.ID)
	removeRoute(c, l, defRoute.ID)
}

func (s *S) TestHTTPInitialSync(c *C) {
	l := s.newHTTPListener(c)
	addHTTPRoute(c, l)
	l.Close()

	srv := httptest.NewServer(httpTestHandler("1"))
	defer srv.Close()

	l = s.newHTTPListener(c)
	defer l.Close()

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	assertGet(c, "http://"+l.Addrs[0], "example.com", "1")
	assertGet(c, "https://"+l.TLSAddrs[0], "example.com", "1")
}

func (s *S) TestHTTPResync(c *C) {
	var connPids []int32
	var cmu sync.Mutex

	poolConfig := newPgxConnPoolConfig()
	poolConfig.AfterConnect = func(conn *pgx.Conn) error {
		cmu.Lock()
		defer cmu.Unlock()
		connPids = append(connPids, conn.Pid)
		return schema.PrepareStatements(conn)
	}
	pgxpool, err := pgx.NewConnPool(poolConfig)
	if err != nil {
		c.Fatal(err)
	}
	l := &HTTPListener{
		Addrs:     []string{"127.0.0.1:0"},
		ds:        NewPostgresDataStore("http", pgxpool),
		discoverd: s.discoverd,
	}
	if err := l.Start(); err != nil {
		c.Fatal(err)
	}
	l.defaultPorts = getDefaultPortsFromAddrs(l)
	defer l.Close()

	srv := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv.Close()
	defer srv2.Close()

	route := addRoute(c, l, router.HTTPRoute{
		Domain:  "example.com",
		Service: "example-com",
	}.ToRoute())
	discoverdRegisterHTTPService(c, l, "example-com", srv.Listener.Addr().String())
	assertGet(c, "http://"+l.Addrs[0], "example.com", "1")

	// testing hooks
	presyncc := make(chan struct{})
	l.preSync = func() {
		<-presyncc
	}

	postsyncc := make(chan struct{})
	l.postSync = func(startc <-chan struct{}) {
		<-startc
		close(postsyncc)
	}

	terminateAllPids := func() {
		cmu.Lock()
		defer cmu.Unlock()

		// grab a fresh conn
		conn, err := pgx.Connect(poolConfig.ConnConfig)
		if err != nil {
			c.Fatal(err)
		}
		defer conn.Close()

		// terminate all conns from the pool
		for _, pid := range connPids {
			if _, err := conn.Exec("select pg_terminate_backend($1)", pid); err != nil {
				c.Fatalf("Unable to kill backend PostgreSQL process: %v", err)
			}
		}
		connPids = connPids[:0]
	}
	terminateAllPids()

	// Because we terminated all of the connections some will be dead, others
	// will become dead when we try to write to them.
	attempts := 0
	for {
		if attempts > 5 {
			c.Fatal(fmt.Errorf("Unable to remove route after disconnecting sync"))
		}
		err = l.RemoveRoute(route.ID)
		if e, ok := err.(*net.OpError); ok {
			if ee, ok := e.Err.(*os.SyscallError); ok && ee.Err == syscall.EPIPE || e.Err == syscall.EPIPE {
				attempts++
				continue
			}
		}
		if err == pgx.ErrDeadConn {
			attempts++
			continue
		}
		c.Assert(err, IsNil)
		break
	}

	// Also add a new route
	err = l.AddRoute(router.HTTPRoute{
		Domain:  "example.org",
		Service: "example-org",
	}.ToRoute())
	c.Assert(err, IsNil)

	// trigger the reconnect
	close(presyncc)
	// wait for the sync to complete
	<-postsyncc

	// ensure that route was actually removed
	res, err := httpClient.Do(newReq("http://"+l.Addrs[0], "example.com"))
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 404)
	res.Body.Close()

	// ensure that new route was added and traffic being directed
	discoverdRegisterHTTPService(c, l, "example-org", srv2.Listener.Addr().String())
	assertGet(c, "http://"+l.Addrs[0], "example.org", "2")
}

// issue #26
func (s *S) TestHTTPServiceHandlerBackendConnectionClosed(c *C) {
	srv := httptest.NewServer(httpTestHandler("1"))

	l := s.newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	// a single request is allowed to successfully get issued
	assertGet(c, "http://"+l.Addrs[0], "example.com", "1")

	// the backend server's connection gets closed, but router is
	// able to recover
	srv.CloseClientConnections()
	// Though we've closed the conn on the server, the client might not have
	// handled the FIN yet. The Transport offers no way to safely retry in those
	// scenarios, so instead we just sleep long enough to handle the FIN.
	// https://golang.org/issue/4677
	time.Sleep(500 * time.Microsecond)
	assertGet(c, "http://"+l.Addrs[0], "example.com", "1")
}

// Act as an app to test HTTP headers
func httpHeaderTestHandler(c *C, ip, port string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c.Assert(req.Header["X-Forwarded-Port"][0], Equals, port)
		c.Assert(req.Header["X-Forwarded-Proto"][0], Equals, "http")
		c.Assert(len(req.Header["X-Request-Start"][0]), Equals, 13)
		c.Assert(req.Header["X-Forwarded-For"][0], Equals, ip)
		c.Assert(req.Header["X-Request-Id"][0], Matches, UUIDRegex)
		w.Write([]byte("1"))
	})
}

// issue #105
func (s *S) TestHTTPHeaders(c *C) {
	l := s.newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	port := mustPortFromAddr(l.listeners[0].Addr().String())
	srv := httptest.NewServer(httpHeaderTestHandler(c, "127.0.0.1", port))

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	assertGet(c, "http://"+l.Addrs[0], "example.com", "1")
}

func (s *S) TestHTTPHeadersFromClient(c *C) {
	l := s.newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	port := mustPortFromAddr(l.listeners[0].Addr().String())
	srv := httptest.NewServer(httpHeaderTestHandler(c, "192.168.1.1, 127.0.0.1", port))

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	req := newReq("http://"+l.Addrs[0], "example.com")
	req.Header.Set("X-Forwarded-For", "192.168.1.1")
	req.Header.Set("X-Request-Id", "asdf1234asdf")
	res, err := httpClient.Do(req)
	c.Assert(err, IsNil)
	defer res.Body.Close()
	c.Assert(res.StatusCode, Equals, 200)
}

func (s *S) TestClientProvidedRequestID(c *C) {
	l := s.newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/false" {
			if !regexp.MustCompile(UUIDRegex).MatchString(req.Header.Get("X-Request-Id")) {
				w.WriteHeader(400)
			}
		} else {
			if req.Header.Get("X-Request-Id") != req.URL.Query().Get("id") {
				w.WriteHeader(400)
			}
		}
	}))

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	for _, t := range []struct {
		id string
		ok bool
	}{
		{"", false},
		{"a", false},
		{strings.Repeat("a", 19), false},
		{strings.Repeat("a", 20), true},
		{strings.Repeat("a", 200), true},
		{strings.Repeat("a", 201), false},
		{"/-abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+=", true},
		{"/-abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+=*", false},
	} {
		req := newReq(fmt.Sprintf("http://%s/%t?id=%s", l.Addrs[0], t.ok, url.QueryEscape(t.id)), "example.com")
		req.Header.Set("X-Request-Id", t.id)
		res, err := httpClient.Do(req)
		c.Assert(err, IsNil)
		res.Body.Close()
		c.Assert(res.StatusCode, Equals, 200, Commentf("id = %q", t.id))
	}
}

func (s *S) TestHTTPProxyHeadersFromClient(c *C) {
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c.Assert(req.Header.Get("Proxy-Authenticate"), Equals, "fake")
		c.Assert(req.Header.Get("Proxy-Authorization"), Equals, "not-empty")
	})
	srv := httptest.NewServer(h)
	defer srv.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)
	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	tests := []struct {
		upgrade bool
	}{
		{upgrade: false}, // regular path
		{upgrade: true},  // tcp/websocket path
	}
	for _, test := range tests {
		req := newReq("http://"+l.Addrs[0], "example.com")
		req.Header.Set("Proxy-Authenticate", "fake")
		req.Header.Set("Proxy-Authorization", "not-empty")
		if test.upgrade {
			req.Header.Set("Connection", "upgrade")
		}
		res, err := httpClient.Do(req)
		c.Assert(err, IsNil)
		defer res.Body.Close()
		c.Assert(res.StatusCode, Equals, 200)
	}
}

func (s *S) TestConnectionCloseHeaderFromClient(c *C) {
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Connection: close header should be stripped by the reverse proxy so it
		// always does keep-alive with backends.
		c.Assert(req.Close, Equals, false)
	})
	srv := httptest.NewServer(h)
	defer srv.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)
	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	req := newReq("http://"+l.Addrs[0], "example.com")
	req.Header.Set("Connection", "close")
	res, err := httpClient.Do(req)
	c.Assert(err, IsNil)
	defer res.Body.Close()
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(res.Close, Equals, true)
}

func (s *S) TestConnectionHeaders(c *C) {
	srv := httptest.NewServer(httpTestHandler("ok"))
	defer srv.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)
	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	tests := []struct {
		conn              string   // connection header string to send, if any
		upgradeFromClient bool     // for server tests, whether to send an upgrade request
		emptyHeaders      []string // headers that shouldn't be set
		presentHeaders    []string // headers that should be set
	}{
		{
			conn: "",
			// Upgrade header must be deleted if Connection header != "upgrade".
			// Transfer-Encoding is always deleted before forwarding.
			emptyHeaders: []string{"Transfer-Encoding", "Upgrade"},
			// Keep all others
			presentHeaders: []string{"Another-Option", "Custom-Conn-Header", "Keep-Alive"},
		},
		{
			conn: "keep-alive",
			// Keep-Alive header should be deleted because that's a conn-specific
			// header here. Upgrade still gets deleted b/c Connection != "upgrade".
			emptyHeaders:   []string{"Keep-Alive", "Transfer-Encoding", "Upgrade"},
			presentHeaders: []string{"Another-Option", "Custom-Conn-Header"},
		},
		{
			conn:           "custom-conn-header",
			emptyHeaders:   []string{"Custom-Conn-Header", "Transfer-Encoding", "Upgrade"},
			presentHeaders: []string{"Another-Option", "Keep-Alive"},
		},
		{ // test multiple connection-options
			conn:           "custom-conn-header,   ,another-option   ",
			emptyHeaders:   []string{"Another-Option", "Custom-Conn-Header", "Transfer-Encoding", "Upgrade"},
			presentHeaders: []string{"Keep-Alive"},
		},
		{
			// tcp/websocket path, all headers should be sent to backend (except
			// Transfer-Encoding)
			conn:              "upgrade",
			upgradeFromClient: true,
			emptyHeaders:      []string{"Transfer-Encoding"},
			presentHeaders:    []string{"Custom-Conn-Header", "Keep-Alive", "Upgrade"},
		},
		{
			// tcp/websocket path, all headers should be sent to backend (except
			// Transfer-Encoding)
			conn:              "upGrade, custom-Conn-header,   ,Another-option   ",
			upgradeFromClient: true,
			emptyHeaders:      []string{"Transfer-Encoding"},
			presentHeaders:    []string{"Another-Option", "Custom-Conn-Header", "Keep-Alive", "Upgrade"},
		},
	}

	for _, test := range tests {
		c.Logf("testing client with Connection: %q", test.conn)
		srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			for _, k := range test.emptyHeaders {
				c.Assert(req.Header.Get(k), Equals, "", Commentf("header = %s", k))
			}
			for _, k := range test.presentHeaders {
				c.Assert(req.Header.Get(k), Not(Equals), "", Commentf("header = %s", k))
			}
		})
		req := newReq("http://"+l.Addrs[0], "example.com")
		if test.conn != "" {
			req.Header.Set("Connection", test.conn)
		}
		req.Header.Set("Another-Option", "test-another-option")
		req.Header.Set("Custom-Conn-Header", "test-custom-conn-header")
		req.Header.Set("Keep-Alive", "test-keep-alive")
		req.Header.Set("Transfer-Encoding", "test-transfer-encoding")
		req.Header.Set("Upgrade", "test-upgrade")
		res, err := httpClient.Do(req)
		c.Assert(err, IsNil)
		res.Body.Close()
		c.Assert(res.StatusCode, Equals, 200)
	}

	for _, test := range tests {
		c.Logf("testing server with Connection: %q", test.conn)
		srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if test.conn != "" {
				w.Header().Set("Connection", test.conn)
			}
			w.Header().Set("Keep-Alive", "test-keep-alive")
			w.Header().Set("Another-Option", "test-another-option")
			w.Header().Set("Custom-Conn-Header", "test-custom-conn-header")
			w.Header().Set("Upgrade", "test-upgrade")
		})
		req := newReq("http://"+l.Addrs[0], "example.com")
		if test.upgradeFromClient {
			req.Header.Set("Connection", "upgrade")
			req.Header.Set("Upgrade", "special-proto")
		}
		res, err := httpClient.Do(req)
		c.Assert(err, IsNil)
		res.Body.Close()
		c.Assert(res.StatusCode, Equals, 200)
		for _, k := range test.emptyHeaders {
			c.Assert(res.Header.Get(k), Equals, "", Commentf("header = %s", k))
		}
		for _, k := range test.presentHeaders {
			c.Assert(res.Header.Get(k), Not(Equals), "", Commentf("header = %s", k))
		}
	}
}

func (s *S) TestHTTPWebsocket(c *C) {
	done := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/websocket" {
			w.Write([]byte("not a websocket upgrade\n"))
			return
		}
		websocket.Handler(func(conn *websocket.Conn) {
			_, err := conn.Write([]byte("1"))
			c.Assert(err, IsNil)
			res := make([]byte, 1)
			_, err = conn.Read(res)
			c.Assert(err, IsNil)
			c.Assert(res[0], Equals, byte('2'))
			done <- struct{}{}
		}).ServeHTTP(w, req)
	})
	srv := httptest.NewServer(h)
	defer srv.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	tests := []struct {
		afterKeepAlive bool
	}{
		{afterKeepAlive: false},
		{afterKeepAlive: true}, // ensure that upgrade still works on reused conn
	}
	for _, test := range tests {
		conn, err := net.Dial("tcp", l.Addrs[0])
		c.Assert(err, IsNil)
		defer conn.Close()

		if test.afterKeepAlive {
			req, err := http.NewRequest("GET", "http://example.com", nil)
			c.Assert(err, IsNil)
			err = req.Write(conn)
			c.Assert(err, IsNil)
			res, err := http.ReadResponse(bufio.NewReader(conn), req)
			c.Assert(err, IsNil)
			data, err := ioutil.ReadAll(res.Body)
			c.Assert(err, IsNil)
			res.Body.Close()
			c.Assert(res.StatusCode, Equals, 200)
			c.Assert(string(data), Equals, "not a websocket upgrade\n")
		}

		conf, err := websocket.NewConfig("ws://example.com/websocket", "http://example.net")
		c.Assert(err, IsNil)
		wc, err := websocket.NewClient(conf, conn)
		c.Assert(err, IsNil)

		res := make([]byte, 1)
		_, err = wc.Read(res)
		c.Assert(err, IsNil)
		c.Assert(res[0], Equals, byte('1'))
		_, err = wc.Write([]byte("2"))
		c.Assert(err, IsNil)
		<-done
	}
}

func (s *S) TestUpgradeHeaderIsCaseInsensitive(c *C) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c.Assert(strings.ToLower(req.Header.Get("Connection")), Equals, "upgrade")
		// ensure that Upgrade header is passed along intact
		c.Assert(req.Header.Get("Upgrade"), Equals, "Some-proto-2")
		w.Write([]byte("ok\n"))
	}))
	defer srv.Close()

	l := s.newHTTPListener(c)
	url := "http://" + l.Addrs[0]
	defer l.Close()

	addHTTPRoute(c, l)
	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	values := []string{"upgrade", "Upgrade", "upGradE"}

	for _, value := range values {
		req := newReq(url, "example.com")
		req.Header.Set("Connection", value)
		req.Header.Set("Upgrade", "Some-proto-2")
		res, err := httpClient.Do(req)
		c.Assert(err, IsNil)
		defer res.Body.Close()

		c.Assert(err, IsNil)
		c.Assert(res.StatusCode, Equals, 200)
		data, err := ioutil.ReadAll(res.Body)
		c.Assert(err, IsNil)
		c.Assert(string(data), Equals, "ok\n")
	}

	httpClient.Transport.(*http.Transport).CloseIdleConnections()
}

func (s *S) TestStickyHTTPRoute(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addStickyHTTPRoute(c, l)

	unregister := discoverdRegisterHTTP(c, l, srv1.Listener.Addr().String())

	cookies := assertGet(c, "http://"+l.Addrs[0], "example.com", "1")
	discoverdRegisterHTTP(c, l, srv2.Listener.Addr().String())
	for i := 0; i < 10; i++ {
		resCookies := assertGetCookies(c, "http://"+l.Addrs[0], "example.com", "1", cookies)
		c.Assert(resCookies, HasLen, 0)
		httpClient.Transport.(*http.Transport).CloseIdleConnections()
	}

	unregister()
	for i := 0; i < 10; i++ {
		resCookies := assertGetCookies(c, "http://"+l.Addrs[0], "example.com", "2", cookies)
		c.Assert(resCookies, Not(HasLen), 0)
	}
}

func wsHandshakeTestHandler(id string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.ToLower(req.Header.Get("Connection")) == "upgrade" {
			w.Header().Set("Connection", "Upgrade")
			w.Header().Set("Upgrade", "websocket")
			w.Header().Set("Backend-Id", id)
			w.WriteHeader(http.StatusSwitchingProtocols)
		} else {
			http.NotFound(w, req)
		}
	})
}

func (s *S) TestStickyHTTPRouteWebsocket(c *C) {
	srv1 := httptest.NewServer(wsHandshakeTestHandler("1"))
	srv2 := httptest.NewServer(wsHandshakeTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l := s.newHTTPListener(c)
	url := "http://" + l.Addrs[0]
	defer l.Close()

	addStickyHTTPRoute(c, l)

	var unregister func()
	steps := []struct {
		do        func()
		backend   string
		setCookie bool
	}{
		// step 1: register srv1, assert requests to srv1
		{
			do:        func() { unregister = discoverdRegisterHTTP(c, l, srv1.Listener.Addr().String()) },
			backend:   "1",
			setCookie: true,
		},
		// step 2: register srv2, assert requests stay with srv1
		{
			do:      func() { discoverdRegisterHTTP(c, l, srv2.Listener.Addr().String()) },
			backend: "1",
		},
		// step 3: unregister srv1, assert requests switch to srv2
		{
			do:        func() { unregister() },
			backend:   "2",
			setCookie: true,
		},
	}

	var sessionCookies []*http.Cookie
	for _, step := range steps {
		step.do()

		cookieSet := false
		for i := 0; i < 10; i++ {
			req := newReq(url, "example.com")
			for _, cookie := range sessionCookies {
				req.AddCookie(cookie)
			}
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
			res, err := httpClient.Do(req)
			c.Assert(err, IsNil)
			defer res.Body.Close()

			c.Assert(err, IsNil)
			c.Assert(res.StatusCode, Equals, 101)
			c.Assert(res.Header.Get("Backend-Id"), Equals, step.backend)

			// reuse the session cookie if present
			if len(res.Cookies()) > 0 {
				// TODO(benburkert): instead of assuming that a session cookie is set
				// if a response has cookies, switch back to checking for the session
				// cookie once this test can access proxy.stickyCookie
				sessionCookies = res.Cookies()
				cookieSet = true
			}
		}

		c.Assert(cookieSet, Equals, step.setCookie)

		httpClient.Transport.(*http.Transport).CloseIdleConnections()
	}
}

func (s *S) TestNoBackends(c *C) {
	l := s.newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, router.HTTPRoute{
		Domain:  "example.com",
		Service: "example-com",
	}.ToRoute())

	req := newReq("http://"+l.Addrs[0], "example.com")
	res, err := newHTTPClient("example.com").Do(req)
	c.Assert(err, IsNil)
	defer res.Body.Close()

	c.Assert(res.StatusCode, Equals, 503)
	data, err := ioutil.ReadAll(res.Body)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "Service Unavailable\n")
}

func (s *S) TestNoResponsiveBackends(c *C) {
	l := s.newHTTPListener(c)
	defer l.Close()

	// close both servers immediately
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv1.Close()
	srv2 := httptest.NewServer(httpTestHandler("2"))
	srv2.Close()

	addRoute(c, l, router.HTTPRoute{
		Domain:  "example.com",
		Service: "example-com",
		Sticky:  true,
	}.ToRoute())
	discoverdRegisterHTTPService(c, l, "example-com", srv1.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "example-com", srv2.Listener.Addr().String())

	type ts struct{ upgrade bool }
	tests := []ts{
		{upgrade: false}, // regular path
		{upgrade: true},  // tcp/websocket path
	}

	runTest := func(test ts) {
		c.Log("upgrade:", test.upgrade)
		req := newReq("http://"+l.Addrs[0], "example.com")
		if test.upgrade {
			req.Header.Set("Connection", "Upgrade")
		}
		res, err := newHTTPClient("example.com").Do(req)
		c.Assert(err, IsNil)
		defer res.Body.Close()

		c.Assert(res.StatusCode, Equals, 503)
		data, err := ioutil.ReadAll(res.Body)
		c.Assert(err, IsNil)
		c.Assert(string(data), Equals, "Service Unavailable\n")
	}

	for _, test := range tests {
		runTest(test)
	}
}

func (s *S) TestClosedBackendRetriesAnotherBackend(c *C) {
	l := s.newHTTPListener(c)
	defer l.Close()

	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv2.Close()

	addRoute(c, l, router.HTTPRoute{
		Domain:  "example.com",
		Service: "example-com",
		Sticky:  true,
	}.ToRoute())
	discoverdRegisterHTTPService(c, l, "example-com", srv1.Listener.Addr().String())
	cookies := assertGet(c, "http://"+l.Addrs[0], "example.com", "1")

	// close srv1, register srv2
	srv1.Close()
	discoverdRegisterHTTPService(c, l, "example-com", srv2.Listener.Addr().String())

	type ts struct {
		method  string
		upgrade bool // whether to trigger the Upgrade/websocket path
	}
	tests := []ts{
		{method: "GET", upgrade: false},
		{method: "GET", upgrade: true},
		{method: "POST", upgrade: false},
		{method: "POST", upgrade: true},
	}

	runTest := func(test ts) {
		c.Log("method:", test.method, "upgrade:", test.upgrade)
		var body io.Reader
		if test.method == "POST" {
			body = strings.NewReader("A not-so-large Flynn test body...")
		}
		req, _ := http.NewRequest(test.method, "http://"+l.Addrs[0], body)
		req.Host = "example.com"
		if test.upgrade {
			req.Header.Set("Connection", "upgrade")
		}
		// add cookies to stick to srv1
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}

		res, err := newHTTPClient("example.com").Do(req)
		c.Assert(err, IsNil)
		defer res.Body.Close()

		c.Assert(res.StatusCode, Equals, 200)
		data, err := ioutil.ReadAll(res.Body)
		c.Assert(err, IsNil)
		c.Assert(string(data), Equals, "2")
		// ensure that unsuccessful upgrades are closed, and non-upgrades aren't.
		c.Assert(res.Close, Equals, test.upgrade)
	}
	for _, test := range tests {
		runTest(test)
	}
}

// Note: this behavior may change if the following issue is fixed, in which case
// this behavior would only apply to non-idempotent requests (i.e. POST):
// https://golang.org/issue/4677
func (s *S) TestErrorAfterConnOnlyHitsOneBackend(c *C) {
	tests := []struct {
		upgrade bool
	}{
		{upgrade: false}, // regular path
		{upgrade: true},  // tcp/websocket path
	}
	for _, test := range tests {
		s.runTestErrorAfterConnOnlyHitsOneBackend(c, test.upgrade)
	}
}

func (s *S) runTestErrorAfterConnOnlyHitsOneBackend(c *C, upgrade bool) {
	c.Log("upgrade:", upgrade)
	closec := make(chan struct{})
	defer close(closec)
	hitCount := 0
	acceptOnlyOnce := func(listener net.Listener) {
		for {
			conn, err := listener.Accept()
			select {
			case <-closec:
				return
			default:
				if err != nil {
					c.Fatal("accept error")
				}
				hitCount++
				conn.Close()
				if hitCount > 1 {
					c.Fatal("received a second conn")
				}
			}
		}
	}
	srv1, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, IsNil)
	defer srv1.Close()
	srv2, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, IsNil)
	defer srv2.Close()

	go acceptOnlyOnce(srv1)
	go acceptOnlyOnce(srv2)

	l := s.newHTTPListener(c)
	defer l.Close()

	defer removeHTTPRoute(c, l, addHTTPRoute(c, l).ID)

	discoverdRegisterHTTP(c, l, srv1.Addr().String())
	discoverdRegisterHTTP(c, l, srv2.Addr().String())

	req := newReq("http://"+l.Addrs[0], "example.com")
	if upgrade {
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
	}
	res, err := newHTTPClient("example.com").Do(req)
	c.Assert(err, IsNil)
	defer res.Body.Close()

	c.Assert(res.StatusCode, Equals, 503)
	data, err := ioutil.ReadAll(res.Body)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "Service Unavailable\n")
}

// issue #152
func (s *S) TestKeepaliveHostname(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, router.HTTPRoute{
		Domain:  "example.com",
		Service: "example-com",
	}.ToRoute())
	addRoute(c, l, router.HTTPRoute{
		Domain:  "example.org",
		Service: "example-org",
	}.ToRoute())

	discoverdRegisterHTTPService(c, l, "example-com", srv1.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "example-org", srv2.Listener.Addr().String())

	assertGet(c, "http://"+l.Addrs[0], "example.com", "1")
	assertGet(c, "http://"+l.Addrs[0], "example.org", "2")
}

// issue #177
func (s *S) TestRequestURIEscaping(c *C) {
	l := s.newHTTPListener(c)
	defer l.Close()
	var prefix string
	uri := "/O08YqxVCf6KRJM6I8p594tzJizQ=/200x300/filters:no_upscale()/http://i.imgur.com/Wru0cNM.jpg?foo=bar"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c.Assert(req.RequestURI, Equals, prefix+uri)
	}))
	defer srv.Close()

	addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	for _, prefix = range []string{"", "http://example.com"} {
		conn, err := net.Dial("tcp", l.Addrs[0])
		c.Assert(err, IsNil)
		defer conn.Close()

		fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: example.com\r\n\r\n", prefix+uri)
		res, err := http.ReadResponse(bufio.NewReader(conn), nil)
		c.Assert(err, IsNil)
		c.Assert(res.StatusCode, Equals, 200)
	}
}

func (s *S) TestRequestQueryParams(c *C) {
	l := s.newHTTPListener(c)
	defer l.Close()

	req := newReq(fmt.Sprintf("http://%s/query", l.Addrs[0]), "example.com")
	req.URL.RawQuery = "first=this+is+a+field&second=was+it+clear+%28already%29%3F"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, inreq *http.Request) {
		c.Assert(inreq.URL.RawQuery, Not(Equals), "")
		c.Assert(inreq.URL.RawQuery, Equals, req.URL.RawQuery)
		c.Assert(inreq.URL.Query().Encode(), Equals, req.URL.Query().Encode())
		c.Assert(inreq.URL.Query().Get("first"), Equals, "this is a field")
		c.Assert(inreq.URL.Query().Get("second"), Equals, "was it clear (already)?")
	}))
	defer srv.Close()

	addHTTPRoute(c, l)
	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	res, err := newHTTPClient("example.com").Do(req)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
}

func (s *S) TestDefaultServerKeypair(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, router.HTTPRoute{
		Domain:  "example.com",
		Service: "example-com",
	}.ToRoute())
	addRoute(c, l, router.HTTPRoute{
		Domain:  "foo.example.com",
		Service: "foo-example-com",
	}.ToRoute())

	discoverdRegisterHTTPService(c, l, "example-com", srv1.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "foo-example-com", srv2.Listener.Addr().String())

	assertGet(c, "https://"+l.TLSAddrs[0], "example.com", "1")
	assertGet(c, "https://"+l.TLSAddrs[0], "foo.example.com", "2")
}

func (s *S) TestCaseInsensitiveDomain(c *C) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(req.Host))
	}))
	defer srv.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, router.HTTPRoute{
		Domain:  "exaMple.com",
		Service: "example-com",
	}.ToRoute())

	discoverdRegisterHTTPService(c, l, "example-com", srv.Listener.Addr().String())

	assertGet(c, "http://"+l.Addrs[0], "Example.com", "Example.com")
	assertGet(c, "https://"+l.TLSAddrs[0], "ExamPle.cOm", "ExamPle.cOm")
}

func (s *S) TestHostPortStripping(c *C) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(req.Host))
	}))
	defer srv.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, router.HTTPRoute{
		Domain:  "example.com",
		Service: "example-com",
	}.ToRoute())

	discoverdRegisterHTTPService(c, l, "example-com", srv.Listener.Addr().String())

	assertGet(c, "http://"+l.Addrs[0], "example.com:80", "example.com:80")
	assertGet(c, "https://"+l.TLSAddrs[0], "example.com:443", "example.com:443")
}

func (s *S) TestHTTPResponseStreaming(c *C) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/body" {
			w.Write([]byte("a"))
		} else {
			w.WriteHeader(200)
		}
		w.(http.Flusher).Flush()
		<-done
	}))
	defer srv.Close()
	defer close(done)

	l := s.newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, router.HTTPRoute{
		Domain:  "example.com",
		Service: "example-com",
	}.ToRoute())

	discoverdRegisterHTTPService(c, l, "example-com", srv.Listener.Addr().String())

	client := newHTTPClient("example.com")
	client.Timeout = 1 * time.Second

	// ensure that we get a flushed response header with no body written immediately
	req := newReq(fmt.Sprintf("http://%s/header", l.Addrs[0]), "example.com")
	res, err := client.Do(req)
	c.Assert(err, IsNil)
	defer res.Body.Close()

	// ensure that we get a body write immediately
	req = newReq(fmt.Sprintf("http://%s/body", l.Addrs[0]), "example.com")
	res, err = client.Do(req)
	c.Assert(err, IsNil)
	defer res.Body.Close()
	buf := make([]byte, 1)
	_, err = res.Body.Read(buf)
	c.Assert(err, IsNil)
	c.Assert(string(buf), Equals, "a")
}

func (s *S) TestHTTPHijackUpgrade(c *C) {
	h := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Connection", "upgrade")
		rw.Header().Set("Upgrade", "pinger")
		rw.WriteHeader(101)

		conn, bufrw, _ := rw.(http.Hijacker).Hijack()
		defer conn.Close()

		line, _, err := bufrw.ReadLine()
		c.Assert(err, IsNil)
		c.Assert(string(line), Equals, "ping!")

		bufrw.Write([]byte("pong!\n"))
		bufrw.Flush()
	})

	srv := httptest.NewServer(http.HandlerFunc(h))
	defer srv.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, router.HTTPRoute{
		Domain:  "127.0.0.1", // TODO: httpclient overrides the Host header
		Service: "example-com",
	}.ToRoute())
	discoverdRegisterHTTPService(c, l, "example-com", srv.Listener.Addr().String())

	client := httpclient.Client{
		URL:  "http://" + l.Addrs[0],
		HTTP: http.DefaultClient,
	}

	rwc, err := client.Hijack("GET", "/", nil, nil)
	c.Assert(err, IsNil)

	rwc.Write([]byte("ping!\n"))

	pong, err := ioutil.ReadAll(rwc)
	c.Assert(err, IsNil)
	c.Assert(string(pong), Equals, "pong!\n")
}

func (s *S) TestHTTPCloseNotify(c *C) {
	success := make(chan struct{})
	done := make(chan struct{})
	cancel := make(chan struct{})
	defer close(done)
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		select {
		case <-w.(http.CloseNotifier).CloseNotify():
			close(success)
		case <-done:
		}
	})
	srv := httptest.NewServer(h)
	defer srv.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)
	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	req := newReq("http://"+l.Addrs[0], "example.com")
	req.Method = "POST"
	req.Cancel = cancel
	res, err := httpClient.Do(req)
	c.Assert(err, IsNil)
	defer res.Body.Close()
	close(cancel)

	select {
	case <-success:
	case <-time.After(10 * time.Second):
		c.Fatal("CloseNotify not called")
	}
}

func (s *S) TestDoubleSlashPath(c *C) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(req.URL.Path))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	assertGet(c, "http://"+l.Addrs[0]+"//foo/bar", "example.com", "//foo/bar")
}

func (s *S) TestHTTPProxyProtocol(c *C) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(req.Header.Get("X-Forwarded-For")))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	l := s.buildHTTPListener(c)
	l.proxyProtocol = true
	c.Assert(l.Start(), IsNil)
	l.defaultPorts = getDefaultPortsFromAddrs(l)
	defer l.Close()

	addHTTPRoute(c, l)
	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	req := newReq("http://"+l.Addrs[0], "example.com")

	dialer := func(addr string) func() net.Conn {
		return func() net.Conn {
			conn, err := net.Dial("tcp", addr)
			c.Assert(err, IsNil)
			conn.Write([]byte("PROXY TCP4 1.1.1.123 20.2.2.2 1000 2000\r\n"))
			return conn
		}
	}

	for _, f := range []func() net.Conn{
		dialer(l.Addrs[0]),
		func() net.Conn {
			return tls.Client(dialer(l.TLSAddrs[0])(), newHTTPClient("example.com").Transport.(*http.Transport).TLSClientConfig)
		},
	} {
		conn := f()
		defer conn.Close()
		c.Assert(req.Write(conn), IsNil)
		res, err := http.ReadResponse(bufio.NewReader(conn), req)
		c.Assert(err, IsNil)
		data, _ := ioutil.ReadAll(res.Body)
		c.Assert(string(data), Equals, "1.1.1.123")
	}
}

func (s *S) TestLegacyTLSDisallowed(c *C) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	l := s.newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())

	config := newHTTPClient("example.com").Transport.(*http.Transport).TLSClientConfig
	config.MaxVersion = tls.VersionTLS10
	conn, err := net.Dial("tcp", l.TLSAddrs[0])
	c.Assert(err, IsNil)
	defer conn.Close()
	client := tls.Client(conn, config)
	err = client.Handshake()
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, ".+protocol version not supported")
}
