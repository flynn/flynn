// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP reverse proxy handler

package proxy

import (
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/inconshreveable/log15"
	"golang.org/x/net/context"
)

const (
	stickyCookie         = "_backend"
	ctxKeyRequestTracker = "_request_tracker"
)

// onExitFlushLoop is a callback set by tests to detect the state of the
// flushLoop() goroutine.
var onExitFlushLoop func()

// Hop-by-hop headers. These are removed when sent to the backend.
// https://tools.ietf.org/html/rfc7230#section-6.1
var (
	hopHeaders = []string{
		"Te", // canonicalized version of "TE"
		"Trailers",
		"Transfer-Encoding",
	}

	serviceUnavailable = []byte("Service Unavailable\n")
)

// ReverseProxy is an HTTP Handler that takes an incoming request and
// sends it to another server, proxying the response back to the
// client.
type ReverseProxy struct {
	// The transport used to perform proxy requests.
	transport *transport

	// FlushInterval specifies the flush interval
	// to flush to the client while copying the
	// response body.
	// If zero, no periodic flushing is done.
	FlushInterval time.Duration

	RequestTracker RequestTracker

	// Logger is the logger for the proxy.
	Logger log15.Logger

	Error503Page []byte
}

type RequestTracker interface {
	TrackRequestStart(backend string)
	TrackRequestDone(backend string)
}

// NewReverseProxy initializes a new ReverseProxy with a callback to get
// backends, a stickyKey for encrypting sticky session cookies, and a flag
// sticky to enable sticky sessions.
func NewReverseProxy(bf BackendListFunc, stickyKey *[32]byte, sticky bool, rt RequestTracker, l log15.Logger) *ReverseProxy {
	return &ReverseProxy{
		transport: &transport{
			getBackends:       bf,
			stickyCookieKey:   stickyKey,
			useStickySessions: sticky,
		},
		FlushInterval:  10 * time.Millisecond,
		RequestTracker: rt,
		Logger:         l,
	}
}

// ServeHTTP implements http.Handler.
func (p *ReverseProxy) ServeHTTP(ctx context.Context, rw http.ResponseWriter, req *http.Request) {
	transport := p.transport
	if transport == nil {
		panic("router: nil transport for proxy")
	}

	outreq := prepareRequest(req)

	l := p.Logger.New("request_id", req.Header.Get("X-Request-Id"), "client_addr", req.RemoteAddr, "host", req.Host, "path", req.URL.Path, "method", req.Method)

	if isConnectionUpgrade(req.Header) {
		p.serveUpgrade(rw, l, outreq)
		return
	}

	ctx = context.WithValue(ctx, ctxKeyRequestTracker, p.RequestTracker)

	// CloseNotify can trigger early with HTTP/1.1 pipelined requests. Since
	// there is no way to detect if a request is pipelined, we instead check if
	// the request was made over HTTP/2 or if the method is defined as
	// idempotent. Pipelining is relatively rare in the wild, the most common
	// client that uses pipelining is Safari on iOS, which also supports HTTP/2
	// so this is only relevant when no TLS certificate is configured for the
	// route. There may be obscure non-browser API clients that use
	// POST/PUT/DELETE with pipelining. If this issue comes up again, we can add
	// a route tunable to disable CloseNotify or suggest that the client stop
	// using pipelining or switch to HTTP/2. The issue presents itself as
	// request cancellations that result in 503s in response to random pipelined
	// requests.
	//
	// https://golang.org/issue/13165
	// https://golang.org/pkg/net/http/#CloseNotifier
	if req.ProtoMajor == 2 || (req.Method != "GET" && req.Method != "HEAD") {
		clientGone := rw.(http.CloseNotifier).CloseNotify()
		var cancel func()
		ctx, cancel = context.WithCancel(ctx)
		defer cancel() // finish cancellation goroutine

		go func() {
			select {
			case <-clientGone:
				cancel() // client went away, cancel request
			case <-ctx.Done():
			}
		}()
	}

	res, trace, err := transport.RoundTrip(ctx, outreq, l)
	if err != nil {
		p.serviceUnavailable(rw)
		return
	}
	defer res.Body.Close()
	defer p.RequestTracker.TrackRequestDone(trace.Backend.Addr)

	prepareResponseHeaders(res)
	p.writeResponse(rw, res)
	if location := res.Header.Get("Location"); location != "" {
		l = l.New("location", location)
	}
	if !trace.ReusedConn {
		l = l.New("connect", trace.ConnectDone.Sub(trace.ConnectStart))
	}
	if req.Body != nil {
		l = l.New("write_req_body", trace.BodyWritten.Sub(trace.HeadersWritten))
	}
	l.Debug("request complete",
		"status", res.StatusCode,
		"job.id", trace.Backend.JobID,
		"addr", trace.Backend.Addr,
		"conn_reused", trace.ReusedConn,
		"write_req_headers", trace.HeadersWritten.Sub(trace.ConnectDone),
		"read_res_first_byte", trace.FirstByte.Sub(trace.HeadersWritten),
	)
}

// ServeConn takes an inbound conn and proxies it to a backend.
func (p *ReverseProxy) ServeConn(ctx context.Context, dconn net.Conn) {
	transport := p.transport
	if transport == nil {
		panic("router: nil transport for proxy")
	}
	defer dconn.Close()

	clientGone := dconn.(http.CloseNotifier).CloseNotify()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // finish cancellation goroutine

	go func() {
		select {
		case <-clientGone:
			cancel() // client went away, cancel request
		case <-ctx.Done():
		}
	}()

	l := p.Logger.New("client_addr", dconn.RemoteAddr(), "host_addr", dconn.LocalAddr(), "proxy", "tcp")

	uconn, err := transport.Connect(ctx, l)
	if err != nil {
		return
	}
	defer uconn.Close()

	joinConns(uconn, dconn)
}

func (p *ReverseProxy) serveUpgrade(rw http.ResponseWriter, l log15.Logger, req *http.Request) {
	transport := p.transport
	if transport == nil {
		panic("router: nil transport for proxy")
	}

	res, uconn, err := transport.UpgradeHTTP(req, l)
	if err != nil {
		p.serviceUnavailable(rw)
		return
	}
	defer uconn.Close()

	prepareResponseHeaders(res)
	if res.StatusCode != 101 {
		res.Header.Set("Connection", "close")
		p.writeResponse(rw, res)
		return
	}

	dconn, bufrw, err := rw.(http.Hijacker).Hijack()
	if err != nil {
		l.Error("error hijacking request", "err", err, "status", "503")
		p.serviceUnavailable(rw)
		return
	}
	defer dconn.Close()

	if err := res.Write(dconn); err != nil {
		l.Error("error proxying response to client", "err", err)
		return
	}
	joinConns(uconn, &streamConn{bufrw.Reader, dconn})
}

func (p *ReverseProxy) serviceUnavailable(rw http.ResponseWriter) {
	if len(p.Error503Page) > 0 {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		rw.WriteHeader(http.StatusServiceUnavailable)
		rw.Write(p.Error503Page)
		return
	}
	rw.WriteHeader(http.StatusServiceUnavailable)
	rw.Write(serviceUnavailable)
	return
}

func prepareResponseHeaders(res *http.Response) {
	// remove global hop-by-hop headers.
	for _, h := range hopHeaders {
		res.Header.Del(h)
	}

	// remove the Upgrade header and headers referenced in the Connection
	// header if HTTP < 1.1 or if Connection header didn't contain "upgrade":
	// https://tools.ietf.org/html/rfc7230#section-6.7
	if !res.ProtoAtLeast(1, 1) || !isConnectionUpgrade(res.Header) {
		res.Header.Del("Upgrade")

		// A proxy or gateway MUST parse a received Connection header field before a
		// message is forwarded and, for each connection-option in this field, remove
		// any header field(s) from the message with the same name as the
		// connection-option, and then remove the Connection header field itself (or
		// replace it with the intermediary's own connection options for the
		// forwarded message): https://tools.ietf.org/html/rfc7230#section-6.1
		tokens := strings.Split(res.Header.Get("Connection"), ",")
		for _, hdr := range tokens {
			res.Header.Del(hdr)
		}
		res.Header.Del("Connection")
	}
}

func (p *ReverseProxy) writeResponse(rw http.ResponseWriter, res *http.Response) {
	copyHeader(rw.Header(), res.Header)

	rw.WriteHeader(res.StatusCode)
	p.copyResponse(rw, res.Body)
}

func isConnectionUpgrade(h http.Header) bool {
	for _, token := range strings.Split(h.Get("Connection"), ",") {
		if v := strings.ToLower(strings.TrimSpace(token)); v == "upgrade" {
			return true
		}
	}
	return false
}

func (p *ReverseProxy) copyResponse(dst io.Writer, src io.Reader) {
	if p.FlushInterval != 0 {
		if wf, ok := dst.(writeFlusher); ok {
			mlw := &maxLatencyWriter{
				dst:     wf,
				latency: p.FlushInterval,
				done:    make(chan bool),
			}
			go mlw.flushLoop()
			defer mlw.stop()
			dst = mlw
		}
	}

	io.Copy(dst, src)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

type closeWriter interface {
	CloseWrite() error
}

func closeWrite(conn net.Conn) {
	if cw, ok := conn.(closeWriter); ok {
		cw.CloseWrite()
	} else {
		conn.Close()
	}
}

func joinConns(uconn, dconn net.Conn) {
	done := make(chan struct{})

	go func() {
		io.Copy(uconn, dconn)
		closeWrite(uconn)
		done <- struct{}{}
	}()

	io.Copy(dconn, uconn)
	closeWrite(dconn)
	<-done
}

func prepareRequest(req *http.Request) *http.Request {
	outreq := new(http.Request)
	*outreq = *req // includes shallow copies of maps, but okay

	// Pass the Request-URI verbatim without any modifications.
	//
	// NOTE: An exception must be made if the Request-URI is a path
	// beginning with "//" (e.g. "//foo/bar") because then
	// req.URL.RequestURI() would interpret req.URL.Opaque as being a URI
	// with the scheme stripped and so generate a URI like scheme:opaque
	// (e.g. "http://foo/bar") which would be incorrect, see:
	// https://github.com/golang/go/blob/f75aafd/src/net/url/url.go#L913-L931
	//
	// It is ok to make this exception because the fallback to
	// req.URL.EscapedPath will generate the correct Request-URI.
	if !strings.HasPrefix(req.RequestURI, "//") {
		outreq.URL.Opaque = strings.Split(strings.TrimPrefix(req.RequestURI, req.URL.Scheme+":"), "?")[0]
	}

	outreq.URL.Scheme = "http"
	outreq.Proto = "HTTP/1.1"
	outreq.ProtoMajor = 1
	outreq.ProtoMinor = 1
	outreq.Close = false

	// Remove hop-by-hop headers to the backend.
	outreq.Header = make(http.Header)
	copyHeader(outreq.Header, req.Header)
	for _, h := range hopHeaders {
		outreq.Header.Del(h)
	}

	// remove the Upgrade header and headers referenced in the Connection
	// header if HTTP < 1.1 or if Connection header didn't contain "upgrade":
	// https://tools.ietf.org/html/rfc7230#section-6.7
	if !req.ProtoAtLeast(1, 1) || !isConnectionUpgrade(req.Header) {
		outreq.Header.Del("Upgrade")

		// Especially important is "Connection" because we want a persistent
		// connection, regardless of what the client sent to us.
		outreq.Header.Del("Connection")

		// A proxy or gateway MUST parse a received Connection header field before a
		// message is forwarded and, for each connection-option in this field, remove
		// any header field(s) from the message with the same name as the
		// connection-option, and then remove the Connection header field itself (or
		// replace it with the intermediary's own connection options for the
		// forwarded message): https://tools.ietf.org/html/rfc7230#section-6.1
		tokens := strings.Split(req.Header.Get("Connection"), ",")
		for _, hdr := range tokens {
			outreq.Header.Del(hdr)
		}
	}

	return outreq
}

type writeFlusher interface {
	io.Writer
	http.Flusher
}

type maxLatencyWriter struct {
	dst     writeFlusher
	latency time.Duration

	lk   sync.Mutex // protects Write + Flush
	done chan bool
}

func (m *maxLatencyWriter) Write(p []byte) (int, error) {
	m.lk.Lock()
	defer m.lk.Unlock()
	return m.dst.Write(p)
}

func (m *maxLatencyWriter) flushLoop() {
	t := time.NewTicker(m.latency)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			if onExitFlushLoop != nil {
				onExitFlushLoop()
			}
			return
		case <-t.C:
			m.lk.Lock()
			m.dst.Flush()
			m.lk.Unlock()
		}
	}
}

func (m *maxLatencyWriter) stop() { m.done <- true }
