package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"

	cc "github.com/flynn/flynn/controller/client"
)

var client *cc.Client

type request struct {
	req     *http.Request
	res     *http.Response
	reqBody *bytes.Buffer
	resBody *bytes.Buffer
}

type roundTripRecorder struct {
	roundTripper http.RoundTripper
	requests     []*request
	mtx          sync.Mutex
}

func (r *roundTripRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	reqBuf, resBuf := &bytes.Buffer{}, &bytes.Buffer{}
	if req.Body != nil {
		req.Body = readCloser{req.Body, io.TeeReader(req.Body, reqBuf)}
	}
	res, err := r.roundTripper.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	log := &request{req, res, reqBuf, resBuf}
	res.Body = readCloser{res.Body, io.TeeReader(res.Body, resBuf)}
	r.mtx.Lock()
	r.requests = append(r.requests, log)
	r.mtx.Unlock()
	return res, nil
}

type readCloser struct {
	io.Closer
	io.Reader
}

var excludeHeaders = map[string]bool{
	"Host":              true,
	"Content-Length":    true,
	"Transfer-Encoding": true,
	"Trailer":           true,
	"User-Agent":        true,
	"Date":              true,
}

type compiledRequest struct {
	Request struct {
		Method  string            `json:"method,omitempty"`
		URL     string            `json:"url,omitempty"`
		Headers map[string]string `json:"headers,omitempty"`
		Body    string            `json:"body,omitempty"`
	} `json:"request,omitempty"`
	Response struct {
		Headers map[string]string `json:"headers,omitempty"`
		Body    string            `json:"body,omitempty"`
	} `json:"response,omitempty"`
}

func compileRequest(r *request) *compiledRequest {
	res := &compiledRequest{}

	// request
	res.Request.Method = r.req.Method

	uri := r.req.URL.RequestURI()
	if strings.HasPrefix(uri, "http") {
		uri = "/" + strings.SplitN(uri[8:], "/", 2)[1]
	}
	res.Request.URL = uri

	res.Request.Headers = make(map[string]string)
	for k, values := range r.req.Header {
		if excludeHeaders[k] {
			continue
		}
		res.Request.Headers[k] = values[0]
	}

	if r.req.Body != nil {
		res.Request.Body = r.reqBody.String()
	}

	// response
	res.Response.Headers = make(map[string]string)
	for k, values := range r.res.Header {
		if excludeHeaders[k] {
			continue
		}
		res.Response.Headers[k] = values[0]
	}

	resBody := r.resBody.String()
	if len(resBody) > 0 {
		res.Response.Body = resBody
	}

	return res
}

func getRequests() []*request {
	t := client.HTTP.Transport.(*roundTripRecorder)
	reqs := t.requests
	t.requests = t.requests[:0]
	return reqs
}
