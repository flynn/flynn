package health

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	. "github.com/flynn/go-check"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type CheckSuite struct{}

var _ = Suite(&CheckSuite{})

func (CheckSuite) TestTCPSuccess(c *C) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, IsNil)
	defer l.Close()

	go func() {
		conn, err := l.Accept()
		if err != nil && strings.Contains(err.Error(), "use of closed network connection") {
			return
		} else if err != nil {
			panic(err)
		}
		conn.Close()
	}()

	err = (&TCPCheck{Addr: l.Addr().String()}).Check()
	c.Assert(err, IsNil)
}

func (CheckSuite) TestTCPFailure(c *C) {
	err := (&TCPCheck{
		Addr:    "127.0.0.1:65535",
		Timeout: 10 * time.Millisecond,
	}).Check()
	c.Assert(err, NotNil)
	c.Assert(strings.Contains(err.Error(), "connection refused"), Equals, true, Commentf("err = %s", err))
}

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.Host == "example.com" {
		w.WriteHeader(400)
	}
	if r.RequestURI == "/ok" {
		w.Write([]byte("ok"))
	}
})

func (CheckSuite) TestHTTPSuccess(c *C) {
	srv := httptest.NewServer(okHandler)
	defer srv.Close()

	err := (&HTTPCheck{URL: srv.URL}).Check()
	c.Assert(err, IsNil)
}

func (CheckSuite) TestHTTPS(c *C) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS.ServerName != "example.com" {
			w.WriteHeader(400)
			return
		}
	}))
	defer srv.Close()

	err := (&HTTPCheck{
		URL:  srv.URL,
		Host: "example.com",
	}).Check()
	c.Assert(err, IsNil)

	err = (&HTTPCheck{
		URL:  srv.URL,
		Host: "foo.com",
	}).Check()
	c.Assert(err, NotNil)
	c.Assert(strings.Contains(err.Error(), "400"), Equals, true, Commentf("err = %s", err))
}

func (CheckSuite) TestHTTPCustomStatusHost(c *C) {
	srv := httptest.NewServer(okHandler)
	defer srv.Close()

	err := (&HTTPCheck{
		URL:        srv.URL,
		StatusCode: 400,
		Host:       "example.com",
	}).Check()
	c.Assert(err, IsNil)

	err = (&HTTPCheck{URL: srv.URL}).Check()
	c.Assert(err, IsNil)
}

func (CheckSuite) TestHTTPMatch(c *C) {
	srv := httptest.NewServer(okHandler)
	defer srv.Close()

	err := (&HTTPCheck{
		URL:        srv.URL + "/ok",
		MatchBytes: []byte("ok"),
	}).Check()
	c.Assert(err, IsNil)

	err = (&HTTPCheck{
		URL:        srv.URL + "/ok",
		MatchBytes: []byte("notok"),
	}).Check()
	c.Assert(err, NotNil)
	c.Assert(strings.Contains(err.Error(), "did not match"), Equals, true, Commentf("err = %s", err))
}

func (CheckSuite) TestHTTPReadTimeout(c *C) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	err := (&HTTPCheck{
		URL:        srv.URL,
		Timeout:    50 * time.Millisecond,
		MatchBytes: []byte("foo"),
	}).Check()
	c.Assert(err, NotNil)
	if !strings.Contains(err.Error(), "use of closed network connection") && // <=go1.4
		!strings.Contains(err.Error(), "Client.Timeout exceeded while reading body") { // >=go1.5
		c.Fatalf("unexpected error %q", err)
	}
}

func (CheckSuite) TestHTTPConnectRefused(c *C) {
	err := (&HTTPCheck{
		URL:     "http://127.0.0.1:65535",
		Timeout: 100 * time.Millisecond,
	}).Check()
	c.Assert(err, NotNil)
	c.Assert(strings.Contains(err.Error(), "connection refused"), Equals, true, Commentf("err = %s", err))
}

func (CheckSuite) TestHTTPInvalidURL(c *C) {
	err := (&HTTPCheck{URL: "http%:"}).Check()
	c.Assert(err, NotNil)
	c.Assert(strings.Contains(err.Error(), "URL"), Equals, true, Commentf("err = %s", err))
}
