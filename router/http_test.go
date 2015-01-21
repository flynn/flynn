package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/websocket"
	"github.com/flynn/flynn/discoverd/testutil/etcdrunner"
	"github.com/flynn/flynn/router/types"
)

var httpClient = newHTTPClient("example.com")

// borrowed from net/http/httptest/server.go
// localhostCert is a PEM-encoded TLS cert with SAN IPs
// "127.0.0.1" and "[::1]", expiring at the last second of 2049 (the end
// of ASN.1 time).
// generated from src/pkg/crypto/tls:
// go run generate_cert.go  --rsa-bits 512 --host 127.0.0.1,::1,example.com,*.example.com --ca --start-date "Jan 1 00:00:00 1970" --duration=1000000h
var localhostCert = []byte(`-----BEGIN CERTIFICATE-----
MIIBmjCCAUagAwIBAgIRAP5DRqWA/pgvAnbC6gnl82kwCwYJKoZIhvcNAQELMBIx
EDAOBgNVBAoTB0FjbWUgQ28wIBcNNzAwMTAxMDAwMDAwWhgPMjA4NDAxMjkxNjAw
MDBaMBIxEDAOBgNVBAoTB0FjbWUgQ28wXDANBgkqhkiG9w0BAQEFAANLADBIAkEA
t9JXJg6fCMxvBKfLCukH7dnF1nIdCBuurjXxVM69E2+97G3aDBTIm7rXtxilAYib
BwzBtgqPzUVngbmK25cguQIDAQABo3cwdTAOBgNVHQ8BAf8EBAMCAKQwEwYDVR0l
BAwwCgYIKwYBBQUHAwEwDwYDVR0TAQH/BAUwAwEB/zA9BgNVHREENjA0ggtleGFt
cGxlLmNvbYINKi5leGFtcGxlLmNvbYcEfwAAAYcQAAAAAAAAAAAAAAAAAAAAATAL
BgkqhkiG9w0BAQsDQQBJxy1zotHYLZpyoockAlJWRa88hs1PrroUNMlueRtzNkpx
9heaebvotwUkFlnNYJZsfPnO23R0lUlzLJ3p1RNz
-----END CERTIFICATE-----`)

// localhostKey is the private key for localhostCert.
var localhostKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIBOQIBAAJBALfSVyYOnwjMbwSnywrpB+3ZxdZyHQgbrq418VTOvRNvvext2gwU
yJu617cYpQGImwcMwbYKj81FZ4G5ituXILkCAwEAAQJAXvmhp3skdkJSFgCv6qou
O5kqG7uH/nl3DnG2iA/tJw3SlEPftQyzNk5jcIFSxvr8pu1pj+L1vw5pR68/7fre
xQIhAMM0/bYtVbzW+PPjqAev3TKhMyWkY3t9Qvw5OtgmBQ+PAiEA8RGk9OvMxBbR
8zJmOXminEE2VVE1VF0K0OiFLDG+JzcCIHurptE0B42L5E0ffeTg1hKtben7K8ug
oD+LQmyOKcahAiB05Btab2QQyQfwpsWOpP5GShCwefoj+CGgfr7kWRJdLQIgTMZe
++SKD8ascROyDnZ0Td8wbrFnO0YRPEkwlhn6h0U=
-----END RSA PRIVATE KEY-----`)

func httpTestHandler(id string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(id))
	})
}

type httpListener struct {
	*HTTPListener
	cleanup func()
}

func (l *httpListener) Close() error {
	l.HTTPListener.Close()
	if l.cleanup != nil {
		l.cleanup()
	}
	return nil
}

func newHTTPListenerClients(t etcdrunner.TestingT, etcd EtcdClient, discoverd discoverdClient) (*httpListener, discoverdClient) {
	discoverd, etcd, cleanup := setup(t, etcd, discoverd)
	pair, err := tls.X509KeyPair(localhostCert, localhostKey)
	if err != nil {
		t.Fatal(err)
	}
	l := &httpListener{
		&HTTPListener{
			Addr:      "127.0.0.1:0",
			TLSAddr:   "127.0.0.1:0",
			keypair:   pair,
			ds:        NewEtcdDataStore(etcd, "/router/http/"),
			discoverd: discoverd,
		},
		cleanup,
	}
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	return l, discoverd
}

func newHTTPClient(serverName string) *http.Client {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(localhostCert)

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{ServerName: serverName, RootCAs: pool},
		},
	}
}

func newHTTPListener(t etcdrunner.TestingT) (*httpListener, discoverdClient) {
	return newHTTPListenerClients(t, nil, nil)
}

// https://code.google.com/p/go/issues/detail?id=5381
func (s *S) TestIssue5381(c *C) {
	srv := httptest.NewServer(httpTestHandler(""))
	defer srv.Close()

	l, discoverd := newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	assertGet(c, "http://"+l.Addr, "example.com", "")
}

func (s *S) TestAddHTTPRoute(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l, discoverd := newHTTPListener(c)
	defer l.Close()

	r := addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv1.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	assertGet(c, "http://"+l.Addr, "example.com", "1")
	assertGet(c, "https://"+l.TLSAddr, "example.com", "1")

	discoverdUnregister(c, discoverd, "test", srv1.Listener.Addr().String())
	discoverdRegisterHTTP(c, l, srv2.Listener.Addr().String())

	// Close the connection we just used to trigger a new backend choice
	httpClient.Transport.(*http.Transport).CloseIdleConnections()

	assertGet(c, "http://"+l.Addr, "example.com", "2")
	assertGet(c, "https://"+l.TLSAddr, "example.com", "2")

	res, err := httpClient.Do(newReq("http://"+l.Addr, "example2.com"))
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 404)
	res.Body.Close()

	_, err = (&http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{ServerName: "example2.com"}},
	}).Do(newReq("https://"+l.TLSAddr, "example2.com"))
	c.Assert(err, Not(IsNil))

	wait := waitForEvent(c, l, "remove", r.ID)
	err = l.RemoveRoute(r.ID)
	c.Assert(err, IsNil)
	wait()
	httpClient.Transport.(*http.Transport).CloseIdleConnections()

	res, err = httpClient.Do(newReq("http://"+l.Addr, "example.com"))
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 404)
	res.Body.Close()
}

func newReq(url, host string) *http.Request {
	req, _ := http.NewRequest("GET", url, nil)
	req.Host = host
	return req
}

func assertGet(c *C, url, host, expected string) *http.Cookie {
	return assertGetCookie(c, url, host, expected, nil)
}

func assertGetCookie(c *C, url, host, expected string, cookie *http.Cookie) *http.Cookie {
	req := newReq(url, host)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := newHTTPClient(host).Do(req)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	data, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, expected)
	for _, c := range res.Cookies() {
		if c.Name == stickyCookie {
			return c
		}
	}
	return nil
}

func addHTTPRoute(c *C, l *httpListener) *router.Route {
	return addRoute(c, l, (&router.HTTPRoute{
		Domain:  "example.com",
		Service: "test",
		TLSCert: string(localhostCert),
		TLSKey:  string(localhostKey),
	}).ToRoute())
}

func addStickyHTTPRoute(c *C, l *httpListener) *router.Route {
	return addRoute(c, l, (&router.HTTPRoute{
		Domain:  "example.com",
		Service: "test",
		Sticky:  true,
	}).ToRoute())
}

func (s *S) TestWildcardRouting(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	srv3 := httptest.NewServer(httpTestHandler("3"))
	defer srv1.Close()
	defer srv2.Close()
	defer srv3.Close()

	l, discoverd := newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, (&router.HTTPRoute{
		Domain:  "foo.bar",
		Service: "1",
	}).ToRoute())
	addRoute(c, l, (&router.HTTPRoute{
		Domain:  "*.foo.bar",
		Service: "2",
	}).ToRoute())
	addRoute(c, l, (&router.HTTPRoute{
		Domain:  "dev.foo.bar",
		Service: "3",
	}).ToRoute())

	discoverdRegisterHTTPService(c, l, "1", srv1.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "2", srv2.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "3", srv3.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	assertGet(c, "http://"+l.Addr, "foo.bar", "1")
	assertGet(c, "http://"+l.Addr, "flynn.foo.bar", "2")
	assertGet(c, "http://"+l.Addr, "dev.foo.bar", "3")
}

func (s *S) TestHTTPInitialSync(c *C) {
	etcd, _, cleanup := newEtcd(c)
	defer cleanup()
	l, _ := newHTTPListenerClients(c, etcd, nil)
	addHTTPRoute(c, l)
	l.Close()

	srv := httptest.NewServer(httpTestHandler("1"))
	defer srv.Close()

	l, discoverd := newHTTPListenerClients(c, etcd, nil)
	defer l.Close()

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	assertGet(c, "http://"+l.Addr, "example.com", "1")
	assertGet(c, "https://"+l.TLSAddr, "example.com", "1")
}

// issue #26
func (s *S) TestHTTPServiceHandlerBackendConnectionClosed(c *C) {
	srv := httptest.NewServer(httpTestHandler("1"))

	l, discoverd := newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	// a single request is allowed to successfully get issued
	assertGet(c, "http://"+l.Addr, "example.com", "1")

	// the backend server's connection gets closed, but router is
	// able to recover
	srv.CloseClientConnections()
	// Though we've closed the conn on the server, the client might not have
	// handled the FIN yet. The Transport offers no way to safely retry in those
	// scenarios, so instead we just sleep long enough to handle the FIN.
	// https://golang.org/issue/4677
	time.Sleep(500 * time.Microsecond)
	assertGet(c, "http://"+l.Addr, "example.com", "1")
}

// Act as an app to test HTTP headers
func httpHeaderTestHandler(c *C, ip, port string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c.Assert(req.Header["X-Forwarded-Port"][0], Equals, port)
		c.Assert(req.Header["X-Forwarded-Proto"][0], Equals, "http")
		c.Assert(len(req.Header["X-Request-Start"][0]), Equals, 13)
		c.Assert(req.Header["X-Forwarded-For"][0], Equals, ip)
		c.Assert(len(req.Header["X-Request-Id"][0]), Equals, 32)
		w.Write([]byte("1"))
	})
}

// issue #105
func (s *S) TestHTTPHeaders(c *C) {
	srv := httptest.NewServer(httpHeaderTestHandler(c, "127.0.0.1", "0"))

	l, discoverd := newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	assertGet(c, "http://"+l.Addr, "example.com", "1")
}

func (s *S) TestHTTPHeadersFromClient(c *C) {
	srv := httptest.NewServer(httpHeaderTestHandler(c, "192.168.1.1, 127.0.0.1", "0"))

	l, discoverd := newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	req := newReq("http://"+l.Addr, "example.com")
	req.Header.Set("X-Forwarded-For", "192.168.1.1")
	req.Header.Set("X-Request-Id", "asdf1234asdf")
	res, err := httpClient.Do(req)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
}

func (s *S) TestHTTPWebsocket(c *C) {
	done := make(chan struct{})
	srv := httptest.NewServer(
		websocket.Handler(func(conn *websocket.Conn) {
			_, err := conn.Write([]byte("1"))
			c.Assert(err, IsNil)
			res := make([]byte, 1)
			_, err = conn.Read(res)
			c.Assert(err, IsNil)
			c.Assert(res[0], Equals, byte('2'))
			close(done)
		}),
	)

	l, discoverd := newHTTPListener(c)
	defer l.Close()

	addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	conn, err := net.Dial("tcp", l.Addr)
	c.Assert(err, IsNil)
	defer conn.Close()
	conf, err := websocket.NewConfig("ws://example.com", "http://example.net")
	c.Assert(err, IsNil)
	wc, err := websocket.NewClient(conf, conn)
	c.Assert(err, IsNil)

	res := make([]byte, 1)
	_, err = wc.Read(res)
	c.Assert(err, IsNil)
	c.Assert(res[0], Equals, byte('1'))
	_, err = wc.Write([]byte("2"))
	c.Assert(err, IsNil)
}

func (s *S) TestStickyHTTPRoute(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l, discoverd := newHTTPListener(c)
	defer l.Close()

	addStickyHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv1.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	cookie := assertGet(c, "http://"+l.Addr, "example.com", "1")
	discoverdRegisterHTTP(c, l, srv2.Listener.Addr().String())
	for i := 0; i < 10; i++ {
		resCookie := assertGetCookie(c, "http://"+l.Addr, "example.com", "1", cookie)
		c.Assert(resCookie, IsNil)
		httpClient.Transport.(*http.Transport).CloseIdleConnections()
	}

	discoverdUnregister(c, discoverd, "test", srv1.Listener.Addr().String())
	for i := 0; i < 10; i++ {
		resCookie := assertGetCookie(c, "http://"+l.Addr, "example.com", "2", cookie)
		c.Assert(resCookie, Not(IsNil))
	}
}

// issue #152
func (s *S) TestKeepaliveHostname(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l, discoverd := newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, (&router.HTTPRoute{
		Domain:  "example.com",
		Service: "example-com",
	}).ToRoute())
	addRoute(c, l, (&router.HTTPRoute{
		Domain:  "example.org",
		Service: "example-org",
	}).ToRoute())

	discoverdRegisterHTTPService(c, l, "example-com", srv1.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "example-org", srv2.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	assertGet(c, "http://"+l.Addr, "example.com", "1")
	assertGet(c, "http://"+l.Addr, "example.org", "2")
}

// issue #177
func (s *S) TestRequestURIEscaping(c *C) {
	l, discoverd := newHTTPListener(c)
	defer l.Close()
	var prefix string
	uri := "/O08YqxVCf6KRJM6I8p594tzJizQ=/200x300/filters:no_upscale()/http://i.imgur.com/Wru0cNM.jpg?foo=bar"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c.Assert(req.RequestURI, Equals, prefix+uri)
	}))
	defer srv.Close()

	addHTTPRoute(c, l)

	discoverdRegisterHTTP(c, l, srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	for _, prefix = range []string{"", "http://example.com"} {
		conn, err := net.Dial("tcp", l.Addr)
		c.Assert(err, IsNil)
		defer conn.Close()

		fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: example.com\r\n\r\n", prefix+uri)
		res, err := http.ReadResponse(bufio.NewReader(conn), nil)
		c.Assert(err, IsNil)
		c.Assert(res.StatusCode, Equals, 200)
	}
}

func (s *S) TestDefaultServerKeypair(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l, discoverd := newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, (&router.HTTPRoute{
		Domain:  "example.com",
		Service: "example-com",
	}).ToRoute())
	addRoute(c, l, (&router.HTTPRoute{
		Domain:  "foo.example.com",
		Service: "foo-example-com",
	}).ToRoute())

	discoverdRegisterHTTPService(c, l, "example-com", srv1.Listener.Addr().String())
	discoverdRegisterHTTPService(c, l, "foo-example-com", srv2.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	assertGet(c, "https://"+l.TLSAddr, "example.com", "1")
	assertGet(c, "https://"+l.TLSAddr, "foo.example.com", "2")
}

func (s *S) TestCaseInsensitiveDomain(c *C) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(req.Host))
	}))
	defer srv.Close()

	l, discoverd := newHTTPListener(c)
	defer l.Close()

	addRoute(c, l, (&router.HTTPRoute{
		Domain:  "exaMple.com",
		Service: "example-com",
	}).ToRoute())

	discoverdRegisterHTTPService(c, l, "example-com", srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	assertGet(c, "http://"+l.Addr, "Example.com", "Example.com")
	assertGet(c, "https://"+l.TLSAddr, "ExamPle.cOm", "ExamPle.cOm")
}
