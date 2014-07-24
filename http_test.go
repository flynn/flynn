package main

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"

	"code.google.com/p/go.net/websocket"
	"github.com/flynn/strowger/types"
	. "github.com/titanous/gocheck"
)

var httpClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{ServerName: "example.com"},
	},
}

// borrowed from net/http/httptest/server.go
// localhostCert is a PEM-encoded TLS cert with SAN IPs
// "127.0.0.1" and "[::1]", expiring at the last second of 2049 (the end
// of ASN.1 time).
// generated from src/pkg/crypto/tls:
// go run generate_cert.go  --rsa-bits 512 --host 127.0.0.1,::1,example.com --ca --start-date "Jan 1 00:00:00 1970" --duration=1000000h
var localhostCert = []byte(`-----BEGIN CERTIFICATE-----
MIIBdzCCASOgAwIBAgIBADALBgkqhkiG9w0BAQUwEjEQMA4GA1UEChMHQWNtZSBD
bzAeFw03MDAxMDEwMDAwMDBaFw00OTEyMzEyMzU5NTlaMBIxEDAOBgNVBAoTB0Fj
bWUgQ28wWjALBgkqhkiG9w0BAQEDSwAwSAJBAN55NcYKZeInyTuhcCwFMhDHCmwa
IUSdtXdcbItRB/yfXGBhiex00IaLXQnSU+QZPRZWYqeTEbFSgihqi1PUDy8CAwEA
AaNoMGYwDgYDVR0PAQH/BAQDAgCkMBMGA1UdJQQMMAoGCCsGAQUFBwMBMA8GA1Ud
EwEB/wQFMAMBAf8wLgYDVR0RBCcwJYILZXhhbXBsZS5jb22HBH8AAAGHEAAAAAAA
AAAAAAAAAAAAAAEwCwYJKoZIhvcNAQEFA0EAAoQn/ytgqpiLcZu9XKbCJsJcvkgk
Se6AbGXgSlq+ZCEVo0qIwSgeBqmsJxUu7NCSOwVJLYNEBO2DtIxoYVk+MA==
-----END CERTIFICATE-----`)

// localhostKey is the private key for localhostCert.
var localhostKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIBPAIBAAJBAN55NcYKZeInyTuhcCwFMhDHCmwaIUSdtXdcbItRB/yfXGBhiex0
0IaLXQnSU+QZPRZWYqeTEbFSgihqi1PUDy8CAwEAAQJBAQdUx66rfh8sYsgfdcvV
NoafYpnEcB5s4m/vSVe6SU7dCK6eYec9f9wpT353ljhDUHq3EbmE4foNzJngh35d
AekCIQDhRQG5Li0Wj8TM4obOnnXUXf1jRv0UkzE9AHWLG5q3AwIhAPzSjpYUDjVW
MCUXgckTpKCuGwbJk7424Nb8bLzf3kllAiA5mUBgjfr/WtFSJdWcPQ4Zt9KTMNKD
EUO0ukpTwEIl6wIhAMbGqZK3zAAFdq8DD2jPx+UJXnh0rnOkZBzDtJ6/iN69AiEA
1Aq8MJgTaYsDQWyU/hDq5YkDJc9e9DSCvUIzqxQWMQE=
-----END RSA PRIVATE KEY-----`)

func init() {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(localhostCert)
	httpClient.Transport.(*http.Transport).TLSClientConfig.RootCAs = pool
}

func httpTestHandler(id string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(id))
	})
}

func newHTTPListener(etcd *fakeEtcd) (*HTTPListener, *fakeDiscoverd, error) {
	discoverd := newFakeDiscoverd()
	if etcd == nil {
		etcd = newFakeEtcd()
	}
	l := NewHTTPListener("127.0.0.1:0", "127.0.0.1:0", nil, NewEtcdDataStore(etcd, "/strowger/http/"), discoverd)
	return l, discoverd, l.Start()
}

// https://code.google.com/p/go/issues/detail?id=5381
func (s *S) TestIssue5381(c *C) {
	srv := httptest.NewServer(httpTestHandler(""))
	defer srv.Close()

	l, discoverd, err := newHTTPListener(nil)
	c.Assert(err, IsNil)
	defer l.Close()

	discoverd.Register("test", srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	addHTTPRoute(c, l)
	assertGet(c, "http://"+l.Addr, "example.com", "")
}

func (s *S) TestAddHTTPRoute(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	l, discoverd, err := newHTTPListener(nil)
	c.Assert(err, IsNil)
	defer l.Close()

	discoverd.Register("test", srv1.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	r := addHTTPRoute(c, l)

	assertGet(c, "http://"+l.Addr, "example.com", "1")
	assertGet(c, "https://"+l.TLSAddr, "example.com", "1")

	discoverd.Unregister("test", srv1.Listener.Addr().String())
	discoverd.Register("test", srv2.Listener.Addr().String())

	// Close the connection we just used to trigger a new backend choice
	httpClient.Transport.(*http.Transport).CloseIdleConnections()

	assertGet(c, "http://"+l.Addr, "example.com", "2")
	assertGet(c, "https://"+l.TLSAddr, "example.com", "2")

	wait := waitForEvent(c, l, "remove", r.ID)
	err = l.RemoveRoute(r.ID)
	c.Assert(err, IsNil)
	wait()
	httpClient.Transport.(*http.Transport).CloseIdleConnections()

	res, err := httpClient.Do(newReq("http://"+l.Addr, "example.com"))
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
	res, err := httpClient.Do(req)
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

func addHTTPRoute(c *C, l *HTTPListener) *strowger.Route {
	wait := waitForEvent(c, l, "set", "")
	r := (&strowger.HTTPRoute{
		Domain:  "example.com",
		Service: "test",
		TLSCert: string(localhostCert),
		TLSKey:  string(localhostKey),
	}).ToRoute()
	err := l.AddRoute(r)
	c.Assert(err, IsNil)
	wait()
	return r
}

func addStickyHTTPRoute(c *C, l *HTTPListener) *strowger.Route {
	wait := waitForEvent(c, l, "set", "")
	r := (&strowger.HTTPRoute{
		Domain:  "example.com",
		Service: "test",
		Sticky:  true,
	}).ToRoute()
	err := l.AddRoute(r)
	c.Assert(err, IsNil)
	wait()
	return r
}

func (s *S) TestHTTPInitialSync(c *C) {
	etcd := newFakeEtcd()
	l, _, err := newHTTPListener(etcd)
	addHTTPRoute(c, l)
	c.Assert(err, IsNil)
	l.Close()

	srv := httptest.NewServer(httpTestHandler("1"))
	defer srv.Close()

	l, discoverd, err := newHTTPListener(etcd)
	c.Assert(err, IsNil)
	defer l.Close()

	discoverd.Register("test", srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	assertGet(c, "http://"+l.Addr, "example.com", "1")
	assertGet(c, "https://"+l.TLSAddr, "example.com", "1")
}

// issue #26
func (s *S) TestHTTPServiceHandlerBackendConnectionClosed(c *C) {
	srv := httptest.NewServer(httpTestHandler("1"))

	l, discoverd, err := newHTTPListener(nil)
	c.Assert(err, IsNil)
	defer l.Close()

	discoverd.Register("test", srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	addHTTPRoute(c, l)

	// a single request is allowed to successfully get issued
	assertGet(c, "http://"+l.Addr, "example.com", "1")

	// the backend server's connection gets closed, but strowger
	// is able to recover
	srv.CloseClientConnections()
	assertGet(c, "http://"+l.Addr, "example.com", "1")
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

	l, discoverd, err := newHTTPListener(nil)
	c.Assert(err, IsNil)
	defer l.Close()

	discoverd.Register("test", srv.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	addHTTPRoute(c, l)

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
	defer srv2.Close()

	l, discoverd, err := newHTTPListener(nil)
	c.Assert(err, IsNil)
	defer l.Close()

	discoverd.Register("test", srv1.Listener.Addr().String())
	defer discoverd.UnregisterAll()

	addStickyHTTPRoute(c, l)

	cookie := assertGet(c, "http://"+l.Addr, "example.com", "1")
	discoverd.Register("test", srv2.Listener.Addr().String())
	for i := 0; i < 10; i++ {
		resCookie := assertGetCookie(c, "http://"+l.Addr, "example.com", "1", cookie)
		c.Assert(resCookie, IsNil)
		httpClient.Transport.(*http.Transport).CloseIdleConnections()
	}
	discoverd.Unregister("test", srv1.Listener.Addr().String())
	for i := 0; i < 10; i++ {
		resCookie := assertGetCookie(c, "http://"+l.Addr, "example.com", "2", cookie)
		c.Assert(resCookie, Not(IsNil))
	}
}
