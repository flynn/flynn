package main

import (
	"net/http"
	"net/http/httptest"

	. "github.com/flynn/go-check"
)

var nopHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

func (s *S) TestFwdProtoHandler(c *C) {
	prevForwardedProto := "fakeproto"
	prevForwardedPort := "8080"

	rec := httptest.NewRecorder()
	request, _ := http.NewRequest("GET", "http://test.com", nil)
	request.RemoteAddr = "1.2.3.4:5678"
	h := fwdProtoHandler{Handler: nopHandler, Proto: "https", Port: "443"}
	h.ServeHTTP(rec, request)
	c.Assert(request.Header.Get("X-Forwarded-For"), Equals, "1.2.3.4")
	c.Assert(request.Header.Get("X-Forwarded-Proto"), Equals, "https")
	c.Assert(request.Header.Get("X-Forwarded-Port"), Equals, "443")

	// test with headers already set, make sure we append correctly
	rec = httptest.NewRecorder()
	request, _ = http.NewRequest("GET", "http://test.com", nil)
	request.Header.Set("X-Forwarded-Proto", prevForwardedProto)
	request.Header.Set("X-Forwarded-Port", prevForwardedPort)

	h.ServeHTTP(rec, request)
	c.Assert(request.Header.Get("X-Forwarded-Proto"), Equals, prevForwardedProto+", https")
	c.Assert(request.Header.Get("X-Forwarded-Port"), Equals, prevForwardedPort+", 443")
}
