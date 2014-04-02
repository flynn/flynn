package main

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"time"

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
	l := NewHTTPListener("127.0.0.1:0", "127.0.0.1:0", NewEtcdDataStore(etcd, "/strowger/http/"), discoverd)
	return l, discoverd, l.Start()
}

const waitTimeout = time.Second

func waitForEvent(c *C, l *HTTPListener, event string, domain string) func() {
	ch := make(chan *strowger.Event)
	l.Watch(ch)
	return func() {
		defer l.Unwatch(ch)
		start := time.Now()
		for {
			timeout := waitTimeout - time.Now().Sub(start)
			if timeout <= 0 {
				break
			}
			select {
			case e := <-ch:
				if e.Event == event && e.Domain == domain {
					return
				}
			case <-time.After(timeout):
				break
			}
		}
		c.Errorf("timeout exceeded waiting for %s %s", event, domain)
	}
}

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

	wait := waitForEvent(c, l, "add", "example.com")
	err = l.AddRoute("example.com", "test", string(localhostCert), string(localhostKey))
	c.Assert(err, IsNil)
	wait()

	assertGet(c, "http://"+l.Addr, "example.com", "1")
	assertGet(c, "https://"+l.TLSAddr, "example.com", "1")

	discoverd.Unregister("test", srv1.Listener.Addr().String())
	discoverd.Register("test", srv2.Listener.Addr().String())

	// Close the connection we just used to trigger a new backend choice
	httpClient.Transport.(*http.Transport).CloseIdleConnections()

	assertGet(c, "http://"+l.Addr, "example.com", "2")
	assertGet(c, "https://"+l.TLSAddr, "example.com", "2")

	wait = waitForEvent(c, l, "remove", "example.com")
	err = l.RemoveRoute("example.com")
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

func assertGet(c *C, url, host, expected string) {
	res, err := httpClient.Do(newReq(url, host))
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
	err = l.AddRoute("example.com", "test", string(localhostCert), string(localhostKey))
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
