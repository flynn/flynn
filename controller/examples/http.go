package main

import (
	"bytes"
	"encoding/json"
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
}

func requestMarkdown(r *request) string {
	buf := &bytes.Buffer{}

	// request headers
	buf.Write([]byte("```text\n"))
	buf.WriteString(r.req.Method)
	buf.WriteByte(' ')

	uri := r.req.URL.RequestURI()
	if strings.HasPrefix(uri, "http") {
		uri = "/" + strings.SplitN(uri[8:], "/", 2)[1]
	}
	buf.WriteString(uri)

	buf.WriteString(" HTTP/1.1\r\n")
	r.req.Header.WriteSubset(buf, excludeHeaders)
	buf.Truncate(buf.Len() - 2) // remove the trailing \r\n from headers
	buf.Write([]byte("\n```\n"))

	// request body
	if r.req.Body != nil {
		fence(buf, r.reqBody.Bytes())
	}

	// response headers
	buf.Write([]byte("\n```text\n"))
	buf.WriteString(r.res.Proto)
	buf.WriteByte(' ')
	buf.WriteString(r.res.Status)
	buf.Write([]byte("\r\n"))
	r.res.Header["ETag"] = r.res.Header["Etag"]
	delete(r.res.Header, "Etag")
	r.res.Header.WriteSubset(buf, excludeHeaders)
	buf.Truncate(buf.Len() - 2) // remove the trailing \r\n from headers
	buf.Write([]byte("\n```\n"))

	// response body
	resBody := r.resBody.Bytes()
	if len(resBody) > 0 {
		fence(buf, resBody)
	}

	return buf.String()
}

func fence(buf *bytes.Buffer, data []byte) {
	if len(data) > 0 && (data[0] == '{' || data[0] == '[') {
		buf.Write([]byte("```json\n"))
		json.Indent(buf, data, "", "  ")
	} else {
		buf.Write([]byte("```text\n"))
		buf.Write(data)
	}
	buf.Write([]byte("\n```\n"))
}

func getRequests() []*request {
	t := client.HTTP.Transport.(*roundTripRecorder)
	reqs := t.requests
	t.requests = t.requests[:0]
	return reqs
}
