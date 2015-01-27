package httprecorder

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
)

type Recorder struct {
	Client            *http.Client
	originalTransport http.RoundTripper
}

type CompiledRequest struct {
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

func NewWithClient(client *http.Client) *Recorder {
	r := &Recorder{Client: client}
	r.originalTransport = client.Transport
	if r.originalTransport == nil {
		r.originalTransport = &http.Transport{}
	}
	client.Transport = &roundTripRecorder{RoundTripper: r.originalTransport}
	return r
}

func (r *Recorder) ResetClient() {
	r.Client.Transport = r.originalTransport
}

func (r *Recorder) GetRequests() []*CompiledRequest {
	t := r.Client.Transport.(*roundTripRecorder)
	reqs := t.requests
	t.requests = t.requests[:0]
	compiledReqs := make([]*CompiledRequest, len(reqs))
	for i, r := range reqs {
		compiledReqs[i] = compileRequest(r)
	}
	return compiledReqs
}

type request struct {
	req     *http.Request
	res     *http.Response
	reqBody *bytes.Buffer
	resBody *bytes.Buffer
}

type roundTripRecorder struct {
	RoundTripper http.RoundTripper
	requests     []*request
	mtx          sync.Mutex
}

func (r *roundTripRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	reqBuf, resBuf := &bytes.Buffer{}, &bytes.Buffer{}
	if req.Body != nil {
		req.Body = readCloser{req.Body, io.TeeReader(req.Body, reqBuf)}
	}
	res, err := r.RoundTripper.RoundTrip(req)
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

var excludeHeaders = map[string]struct{}{
	"Host":              {},
	"Content-Length":    {},
	"Transfer-Encoding": {},
	"Trailer":           {},
	"User-Agent":        {},
	"Date":              {},
}

func compileRequest(r *request) *CompiledRequest {
	res := &CompiledRequest{}

	// request
	res.Request.Method = r.req.Method

	uri := r.req.URL
	uriStr := uri.Path
	if uri.RawQuery != "" {
		uriStr = uriStr + "?" + uri.RawQuery
	}
	if uri.Fragment != "" {
		uriStr = uriStr + "#" + uri.Fragment
	}
	res.Request.URL = uriStr

	res.Request.Headers = make(map[string]string, len(r.req.Header))
	for k, values := range r.req.Header {
		if _, ok := excludeHeaders[k]; ok {
			continue
		}
		res.Request.Headers[k] = strings.Join(values, ",")
	}

	if r.req.Body != nil {
		res.Request.Body = r.reqBody.String()
	}

	// response
	res.Response.Headers = make(map[string]string, len(r.res.Header))
	for k, values := range r.res.Header {
		if _, ok := excludeHeaders[k]; ok {
			continue
		}
		res.Response.Headers[k] = strings.Join(values, ",")
	}

	resBody := r.resBody.String()
	if len(resBody) > 0 {
		res.Response.Body = resBody
	}

	return res
}
