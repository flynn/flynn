// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP reverse proxy handler

package proxy

import (
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
)

const (
	stickyCookie = "_backend"
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

	// ErrorLog specifies an optional logger for errors
	// that occur when attempting to proxy the request.
	// If nil, logging goes to os.Stderr via the log package's
	// standard logger.
	ErrorLog *log.Logger
}

// NewReverseProxy initializes a new ReverseProxy with a callback to get
// backends, a stickyKey for encrypting sticky session cookies, and a flag
// sticky to enable sticky sessions.
func NewReverseProxy(bf BackendListFunc, stickyKey *[32]byte, sticky bool) *ReverseProxy {
	return &ReverseProxy{
		transport: &transport{
			getBackends:       bf,
			stickyCookieKey:   stickyKey,
			useStickySessions: sticky,
		},
		FlushInterval: 10 * time.Millisecond,
	}
}

// ServeHTTP implements http.Handler.
func (p *ReverseProxy) ServeHTTP(ctx context.Context, rw http.ResponseWriter, req *http.Request) {
	transport := p.transport
	if transport == nil {
		panic("router: nil transport for proxy")
	}

	outreq := prepareRequest(req)

	if isConnectionUpgrade(req.Header) {
		p.serveUpgrade(rw, outreq)
		return
	}

	clientGone := rw.(http.CloseNotifier).CloseNotify()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // tells cancellation goroutine to exit

	go func() {
		select {
		case <-clientGone:
			cancel() // client went away, cancel request
		case <-ctx.Done():
		}
	}()

	res, err := transport.RoundTrip(ctx, outreq)
	if err != nil {
		if err == errRequestCanceled {
			// TODO(bgentry): anything special to log here?
			return
		}
		p.logf("router: proxy error: %v", err)
		rw.WriteHeader(http.StatusServiceUnavailable)
		rw.Write(serviceUnavailable)
		return
	}
	defer res.Body.Close()

	p.writeResponse(rw, res)
}

// ServeConn takes an inbound conn and proxies it to a backend.
func (p *ReverseProxy) ServeConn(dconn net.Conn) {
	transport := p.transport
	if transport == nil {
		panic("router: nil transport for proxy")
	}
	defer dconn.Close()

	uconn, err := transport.Connect()
	if err != nil {
		p.logf("router: proxy error: %v", err)
		return
	}
	defer uconn.Close()

	joinConns(uconn, dconn)
}

func (p *ReverseProxy) serveUpgrade(rw http.ResponseWriter, req *http.Request) {
	transport := p.transport
	if transport == nil {
		panic("router: nil transport for proxy")
	}

	res, uconn, err := transport.UpgradeHTTP(req)
	if err != nil {
		p.logf("router: proxy error: %v", err)
		rw.WriteHeader(http.StatusServiceUnavailable)
		rw.Write(serviceUnavailable)
		return
	}
	defer uconn.Close()

	p.writeResponse(rw, res)
	res.Body.Close()

	if res.StatusCode != 101 {
		return
	}

	dconn, bufrw, err := rw.(http.Hijacker).Hijack()
	if err != nil {
		p.logf("router: hijack failed: %v", err)
		return
	}
	defer dconn.Close()
	joinConns(uconn, &streamConn{bufrw.Reader, dconn})
}

func (p *ReverseProxy) writeResponse(rw http.ResponseWriter, res *http.Response) {
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

func (p *ReverseProxy) logf(format string, args ...interface{}) {
	if p.ErrorLog != nil {
		p.ErrorLog.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
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

	// Pass the Request-URI verbatim without any modifications
	outreq.URL.Opaque = strings.Split(strings.TrimPrefix(req.RequestURI, req.URL.Scheme+":"), "?")[0]
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
