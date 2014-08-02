// Copyright 2013 The Gorilla Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handlers

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

const (
	ok         = "ok\n"
	notAllowed = "Method not allowed\n"
)

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte(ok))
})

func newRequest(method, url string) *http.Request {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		panic(err)
	}
	return req
}

func TestMethodHandler(t *testing.T) {
	tests := []struct {
		req     *http.Request
		handler http.Handler
		code    int
		allow   string // Contents of the Allow header
		body    string
	}{
		// No handlers
		{newRequest("GET", "/foo"), MethodHandler{}, http.StatusMethodNotAllowed, "", notAllowed},
		{newRequest("OPTIONS", "/foo"), MethodHandler{}, http.StatusOK, "", ""},

		// A single handler
		{newRequest("GET", "/foo"), MethodHandler{"GET": okHandler}, http.StatusOK, "", ok},
		{newRequest("POST", "/foo"), MethodHandler{"GET": okHandler}, http.StatusMethodNotAllowed, "GET", notAllowed},

		// Multiple handlers
		{newRequest("GET", "/foo"), MethodHandler{"GET": okHandler, "POST": okHandler}, http.StatusOK, "", ok},
		{newRequest("POST", "/foo"), MethodHandler{"GET": okHandler, "POST": okHandler}, http.StatusOK, "", ok},
		{newRequest("DELETE", "/foo"), MethodHandler{"GET": okHandler, "POST": okHandler}, http.StatusMethodNotAllowed, "GET, POST", notAllowed},
		{newRequest("OPTIONS", "/foo"), MethodHandler{"GET": okHandler, "POST": okHandler}, http.StatusOK, "GET, POST", ""},

		// Override OPTIONS
		{newRequest("OPTIONS", "/foo"), MethodHandler{"OPTIONS": okHandler}, http.StatusOK, "", ok},
	}

	for i, test := range tests {
		rec := httptest.NewRecorder()
		test.handler.ServeHTTP(rec, test.req)
		if rec.Code != test.code {
			t.Fatalf("%d: wrong code, got %d want %d", i, rec.Code, test.code)
		}
		if allow := rec.HeaderMap.Get("Allow"); allow != test.allow {
			t.Fatalf("%d: wrong Allow, got %s want %s", i, allow, test.allow)
		}
		if body := rec.Body.String(); body != test.body {
			t.Fatalf("%d: wrong body, got %q want %q", i, body, test.body)
		}
	}
}

func TestWriteLog(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		panic(err)
	}
	ts := time.Date(1983, 05, 26, 3, 30, 45, 0, loc)

	// A typical request with an OK response
	req := newRequest("GET", "http://example.com")
	req.RemoteAddr = "192.168.100.5"

	buf := new(bytes.Buffer)
	writeLog(buf, req, ts, http.StatusOK, 100)
	log := buf.String()

	expected := "192.168.100.5 - - [26/May/1983:03:30:45 +0200] \"GET / HTTP/1.1\" 200 100\n"
	if log != expected {
		t.Fatalf("wrong log, got %q want %q", log, expected)
	}

	// Request with an unauthorized user
	req = newRequest("GET", "http://example.com")
	req.RemoteAddr = "192.168.100.5"
	req.URL.User = url.User("kamil")

	buf.Reset()
	writeLog(buf, req, ts, http.StatusUnauthorized, 500)
	log = buf.String()

	expected = "192.168.100.5 - kamil [26/May/1983:03:30:45 +0200] \"GET / HTTP/1.1\" 401 500\n"
	if log != expected {
		t.Fatalf("wrong log, got %q want %q", log, expected)
	}

	// Request with url encoded parameters
	req = newRequest("GET", "http://example.com/test?abc=hello%20world&a=b%3F")
	req.RemoteAddr = "192.168.100.5"

	buf.Reset()
	writeLog(buf, req, ts, http.StatusOK, 100)
	log = buf.String()

	expected = "192.168.100.5 - - [26/May/1983:03:30:45 +0200] \"GET /test?abc=hello%20world&a=b%3F HTTP/1.1\" 200 100\n"
	if log != expected {
		t.Fatalf("wrong log, got %q want %q", log, expected)
	}
}

func TestWriteCombinedLog(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		panic(err)
	}
	ts := time.Date(1983, 05, 26, 3, 30, 45, 0, loc)

	// A typical request with an OK response
	req := newRequest("GET", "http://example.com")
	req.RemoteAddr = "192.168.100.5"
	req.Header.Set("Referer", "http://example.com")
	req.Header.Set(
		"User-Agent",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_8_2) AppleWebKit/537.33 "+
			"(KHTML, like Gecko) Chrome/27.0.1430.0 Safari/537.33",
	)

	buf := new(bytes.Buffer)
	writeCombinedLog(buf, req, ts, http.StatusOK, 100)
	log := buf.String()

	expected := "192.168.100.5 - - [26/May/1983:03:30:45 +0200] \"GET / HTTP/1.1\" 200 100 \"http://example.com\" " +
		"\"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_8_2) " +
		"AppleWebKit/537.33 (KHTML, like Gecko) Chrome/27.0.1430.0 Safari/537.33\"\n"
	if log != expected {
		t.Fatalf("wrong log, got %q want %q", log, expected)
	}

	// Request with an unauthorized user
	req.URL.User = url.User("kamil")

	buf.Reset()
	writeCombinedLog(buf, req, ts, http.StatusUnauthorized, 500)
	log = buf.String()

	expected = "192.168.100.5 - kamil [26/May/1983:03:30:45 +0200] \"GET / HTTP/1.1\" 401 500 \"http://example.com\" " +
		"\"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_8_2) " +
		"AppleWebKit/537.33 (KHTML, like Gecko) Chrome/27.0.1430.0 Safari/537.33\"\n"
	if log != expected {
		t.Fatalf("wrong log, got %q want %q", log, expected)
	}

	// Test with remote ipv6 address
	req.RemoteAddr = "::1"

	buf.Reset()
	writeCombinedLog(buf, req, ts, http.StatusOK, 100)
	log = buf.String()

	expected = "::1 - kamil [26/May/1983:03:30:45 +0200] \"GET / HTTP/1.1\" 200 100 \"http://example.com\" " +
		"\"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_8_2) " +
		"AppleWebKit/537.33 (KHTML, like Gecko) Chrome/27.0.1430.0 Safari/537.33\"\n"
	if log != expected {
		t.Fatalf("wrong log, got %q want %q", log, expected)
	}

	// Test remote ipv6 addr, with port
	req.RemoteAddr = net.JoinHostPort("::1", "65000")

	buf.Reset()
	writeCombinedLog(buf, req, ts, http.StatusOK, 100)
	log = buf.String()

	expected = "::1 - kamil [26/May/1983:03:30:45 +0200] \"GET / HTTP/1.1\" 200 100 \"http://example.com\" " +
		"\"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_8_2) " +
		"AppleWebKit/537.33 (KHTML, like Gecko) Chrome/27.0.1430.0 Safari/537.33\"\n"
	if log != expected {
		t.Fatalf("wrong log, got %q want %q", log, expected)
	}
}

func BenchmarkWriteLog(b *testing.B) {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		b.Fatalf(err.Error())
	}
	ts := time.Date(1983, 05, 26, 3, 30, 45, 0, loc)

	req := newRequest("GET", "http://example.com")
	req.RemoteAddr = "192.168.100.5"

	b.ResetTimer()

	buf := &bytes.Buffer{}
	for i := 0; i < b.N; i++ {
		buf.Reset()
		writeLog(buf, req, ts, http.StatusUnauthorized, 500)
	}
}
