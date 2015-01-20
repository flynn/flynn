package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

var nopHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

func TestFwdProtoHandler(t *testing.T) {
	prevForwardedProto := "fakeproto"
	prevForwardedPort := "8080"

	rec := httptest.NewRecorder()
	request, _ := http.NewRequest("GET", "http://test.com", nil)
	request.RemoteAddr = "1.2.3.4:5678"
	h := fwdProtoHandler{Handler: nopHandler, Proto: "https", Port: "443"}
	h.ServeHTTP(rec, request)
	if v := request.Header.Get("X-Forwarded-For"); v != "1.2.3.4" {
		t.Errorf("want X-Forwarded-For %s, got %s", "1.2.3.4", v)
	}
	if v := request.Header.Get("X-Forwarded-Proto"); v != "https" {
		t.Errorf("want X-Forwarded-Proto %s, got %s", "https", v)
	}
	if v := request.Header.Get("X-Forwarded-Port"); v != "443" {
		t.Errorf("want X-Forwarded-Port %s, got %s", "443", v)
	}

	// test with headers already set, make sure we append correctly
	rec = httptest.NewRecorder()
	request, _ = http.NewRequest("GET", "http://test.com", nil)
	request.Header.Set("X-Forwarded-Proto", prevForwardedProto)
	request.Header.Set("X-Forwarded-Port", prevForwardedPort)

	h.ServeHTTP(rec, request)
	want := prevForwardedProto + ", https"
	got := request.Header.Get("X-Forwarded-Proto")
	if want != got {
		t.Errorf("want X-Forwarded-Proto %q, got %q", want, got)
	}
	want = prevForwardedPort + ", 443"
	got = request.Header.Get("X-Forwarded-Port")
	if want != got {
		t.Errorf("want X-Forwarded-Port %q, got %q", want, got)
	}
}
