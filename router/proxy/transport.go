package proxy

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/flynn/flynn/pkg/random"
	router "github.com/flynn/flynn/router/types"
	"github.com/inconshreveable/log15"
	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/net/context"
)

type backendDialer interface {
	Dial(network, addr string) (c net.Conn, err error)
}

var (
	errNoBackends = errors.New("router: no backends available")
	errCanceled   = errors.New("router: backend connection canceled")

	dialer backendDialer = &net.Dialer{
		Timeout:   1 * time.Second,
		KeepAlive: 30 * time.Second,
	}
)

// maxBackendAttempts is the maximum number of backends to attempt to proxy
// a given request to
const maxBackendAttempts = 4

// BackendListFunc returns a slice of backends
type BackendListFunc func() []*router.Backend

func newHTTPTransport(disableKeepAlives bool) *http.Transport {
	return &http.Transport{
		Dial: customDial,
		// The response header timeout is currently set pretty high because
		// gitreceive doesn't send headers until it is done unpacking the repo,
		// it should be lowered after this is fixed.
		ResponseHeaderTimeout: 10 * time.Minute,
		TLSHandshakeTimeout:   10 * time.Second, // unused, but safer to leave default in place
		DisableKeepAlives:     disableKeepAlives,
	}
}

type transport struct {
	*http.Transport

	getBackends BackendListFunc

	stickyCookieKey   *[32]byte
	useStickySessions bool

	inFlightMtx      sync.Mutex
	inFlightRequests map[string]int64
}

func (t *transport) trackRequestStart(backend *router.Backend) {
	t.inFlightMtx.Lock()
	t.inFlightRequests[backend.Addr]++
	t.inFlightMtx.Unlock()
}

func (t *transport) trackRequestEnd(backend *router.Backend) {
	t.inFlightMtx.Lock()
	t.inFlightRequests[backend.Addr]--
	if t.inFlightRequests[backend.Addr] == 0 {
		delete(t.inFlightRequests, backend.Addr)
	}
	t.inFlightMtx.Unlock()
}

// eachBackend iterates through the given backends and calls the given
// function, returning early if the function returns nil or if the error
// returned is not retryable, iterating through no more than
// maxBackendAttempts.
//
// If stickyBackend matches one of the backends then that backend will be tried
// first.
//
// On each iteration, two random backends are picked and the one with the least
// load is tried, thus implementing the "power of two random choices"
// algorithm.
func (t *transport) eachBackend(stickyBackend string, backends []*router.Backend, l log15.Logger, f func(*router.Backend) error) error {
	// check we have some backends
	if len(backends) == 0 {
		return errNoBackends
	}

	attempt := 0

	// try tries calling f with the backend at the given index, returning
	// the resulting error and whether or not the request can be retried
	try := func(index int) (error, bool) {
		backend := backends[index]
		t.trackRequestStart(backend)
		err := f(backend)
		if err == nil {
			return nil, false
		}
		t.trackRequestEnd(backend)
		if _, ok := err.(dialErr); !ok {
			l.Error("unretriable request error", "status", httpErrStatus(err), "job.id", backend.JobID, "addr", backend.Addr, "err", err, "attempt", attempt)
			return err, false
		}
		l.Error("retriable dial error", "job.id", backend.JobID, "addr", backend.Addr, "err", err, "attempt", attempt)
		// remove the backend now that we've tried it
		backends = append(backends[:index], backends[index+1:]...)
		attempt++
		return err, attempt < maxBackendAttempts
	}

	// prioritise the sticky backend if it exists
	if stickyBackend != "" {
		for index, backend := range backends {
			if backend.Addr == stickyBackend {
				if err, shouldRetry := try(index); err == nil || !shouldRetry {
					return err
				}
				break
			}
		}
	}

	// keep picking two random backends and trying the one with the least
	// number of in flight requests
	for len(backends) > 0 {
		// if there is only one backend, try it and return
		if len(backends) == 1 {
			err, _ := try(0)
			return err
		}

		// pick two distinct random backends
		n1 := random.Math.Intn(len(backends))
		n2 := random.Math.Intn(len(backends))
		if n2 == n1 {
			n2 = (n2 + 1) % len(backends)
		}

		// determine which one has the least number of in flight
		// requests
		t.inFlightMtx.Lock()
		load1 := t.inFlightRequests[backends[n1].Addr]
		load2 := t.inFlightRequests[backends[n2].Addr]
		t.inFlightMtx.Unlock()
		index := n1
		if load1 > load2 {
			index = n2
		}

		// try the chosen backend
		if err, shouldRetry := try(index); err == nil || !shouldRetry {
			return err
		}
	}
	l.Error("request failed", "status", "503", "num_backends", len(backends))
	return errNoBackends
}

func (t *transport) getOrderedBackends(stickyBackend string) []*router.Backend {
	backends := t.getBackends()
	shuffleBackends(backends)

	if stickyBackend != "" {
		swapToFront(backends, stickyBackend)
	}
	return backends
}

func (t *transport) getStickyBackend(req *http.Request) string {
	if t.useStickySessions {
		return getStickyCookieBackend(req, *t.stickyCookieKey)
	}
	return ""
}

func (t *transport) setStickyBackend(res *http.Response, originalStickyBackend string) {
	if !t.useStickySessions {
		return
	}
	if backend := res.Request.URL.Host; backend != originalStickyBackend {
		setStickyCookieBackend(res, backend, *t.stickyCookieKey)
	}
}

func (t *transport) RoundTrip(req *http.Request, l log15.Logger) (*http.Response, *RequestTrace, error) {
	// http.Transport closes the request body on a failed dial, issue #875
	req.Body = &fakeCloseReadCloser{req.Body}
	defer req.Body.(*fakeCloseReadCloser).RealClose()

	// trace the request timings (do not use the trace before the request
	// has been RoundTripped)
	req, trace := traceRequest(req)

	rt := req.Context().Value(ctxKeyRequestTracker).(RequestTracker)
	stickyBackend := t.getStickyBackend(req)
	backends := t.getBackends()

	var res *http.Response
	err := t.eachBackend(stickyBackend, backends, l, func(backend *router.Backend) (err error) {
		req.URL.Host = backend.Addr
		rt.TrackRequestStart(backend.Addr)
		res, err = t.Transport.RoundTrip(req)
		if err == nil {
			trace.Finalize(backend)
			t.setStickyBackend(res, stickyBackend)
			return
		}
		rt.TrackRequestDone(backend.Addr)
		return
	})
	if err == nil {
		return res, trace, nil
	}
	return nil, nil, err
}

func (t *transport) Connect(ctx context.Context, l log15.Logger) (net.Conn, error) {
	backends := t.getOrderedBackends("")
	conn, backend, err := dialTCP(ctx, l, backends)
	if err != nil {
		l.Error("connection failed", "err", err, "num_backends", len(backends), "job.id", backend.JobID, "addr", backend.Addr)
	}
	return conn, err
}

func (t *transport) UpgradeHTTP(req *http.Request, l log15.Logger) (*http.Response, net.Conn, error) {
	stickyBackend := t.getStickyBackend(req)
	backends := t.getOrderedBackends(stickyBackend)
	upconn, backend, err := dialTCP(context.Background(), l, backends)
	if err != nil {
		l.Error("dial failed", "status", "503", "num_backends", len(backends))
		return nil, nil, err
	}
	conn := &streamConn{bufio.NewReader(upconn), upconn}
	req.URL.Host = backend.Addr

	if err := req.Write(conn); err != nil {
		conn.Close()
		l.Error("error writing request", "err", err, "job.id", backend.JobID, "addr", backend.Addr)
		return nil, nil, err
	}
	res, err := http.ReadResponse(conn.Reader, req)
	if err != nil {
		conn.Close()
		l.Error("error reading response", "err", err, "job.id", backend.JobID, "addr", backend.Addr)
		return nil, nil, err
	}
	t.setStickyBackend(res, stickyBackend)
	return res, conn, nil
}

func dialTCP(ctx context.Context, l log15.Logger, backends []*router.Backend) (net.Conn, *router.Backend, error) {
	donec := ctx.Done()
	for i, backend := range backends {
		select {
		case <-donec:
			return nil, nil, errCanceled
		default:
		}
		conn, err := dialer.Dial("tcp", backend.Addr)
		if err == nil {
			return conn, backend, nil
		}
		l.Error("retriable dial error", "job.id", backend.JobID, "addr", backend.Addr, "err", err, "attempt", i)
	}
	return nil, nil, errNoBackends
}

func customDial(network, addr string) (net.Conn, error) {
	conn, err := dialer.Dial(network, addr)
	if err != nil {
		return nil, dialErr{err}
	}
	return conn, nil
}

type dialErr struct {
	error
}

type fakeCloseReadCloser struct {
	io.ReadCloser
}

func (w *fakeCloseReadCloser) Close() error {
	return nil
}

func (w *fakeCloseReadCloser) RealClose() error {
	if w.ReadCloser == nil {
		return nil
	}
	return w.ReadCloser.Close()
}

func shuffleBackends(backends []*router.Backend) {
	for i := len(backends) - 1; i > 0; i-- {
		j := random.Math.Intn(i + 1)
		backends[i], backends[j] = backends[j], backends[i]
	}
}

func swapToFront(backends []*router.Backend, addr string) {
	for i, backend := range backends {
		if backend.Addr == addr {
			backends[0], backends[i] = backends[i], backends[0]
			return
		}
	}
}

func getStickyCookieBackend(req *http.Request, cookieKey [32]byte) string {
	cookie, err := req.Cookie(StickyCookieName)
	if err != nil {
		return ""
	}

	data, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		return ""
	}
	return string(decrypt(data, cookieKey))
}

func setStickyCookieBackend(res *http.Response, backend string, cookieKey [32]byte) {
	cookie := http.Cookie{
		Name:  StickyCookieName,
		Value: base64.StdEncoding.EncodeToString(encrypt([]byte(backend), cookieKey)),
		Path:  "/",
	}
	res.Header.Add("Set-Cookie", cookie.String())
}

func encrypt(data []byte, key [32]byte) []byte {
	var nonce [24]byte
	_, err := io.ReadFull(rand.Reader, nonce[:])
	if err != nil {
		panic(err)
	}

	out := make([]byte, len(nonce), len(nonce)+len(data)+secretbox.Overhead)
	copy(out, nonce[:])
	return secretbox.Seal(out, data, &nonce, &key)
}

func decrypt(data []byte, key [32]byte) []byte {
	var nonce [24]byte
	if len(data) < len(nonce) {
		return nil
	}
	copy(nonce[:], data)
	res, ok := secretbox.Open(nil, data[len(nonce):], &nonce, &key)
	if !ok {
		return nil
	}
	return res
}

type RequestTrace struct {
	Backend        *router.Backend
	mtx            sync.Mutex
	final          bool
	ReusedConn     bool
	WasIdleConn    bool
	ConnectStart   time.Time
	ConnectDone    time.Time
	HeadersWritten time.Time
	BodyWritten    time.Time
	FirstByte      time.Time
}

// Finalize safely finalizes the trace for read access.
func (r *RequestTrace) Finalize(backend *router.Backend) {
	r.mtx.Lock()
	r.final = true
	r.Backend = backend
	r.mtx.Unlock()
}

// traceRequest sets up request tracing and returns the modified
// request.
func traceRequest(req *http.Request) (*http.Request, *RequestTrace) {
	trace := &RequestTrace{}
	ct := &httptrace.ClientTrace{
		GetConn: func(hostPort string) {
			trace.mtx.Lock()
			defer trace.mtx.Unlock()
			if trace.final {
				return
			}
			trace.ConnectStart = time.Now()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			trace.mtx.Lock()
			defer trace.mtx.Unlock()
			if trace.final {
				return
			}
			trace.ConnectDone = time.Now()
			trace.ReusedConn = info.Reused
			trace.WasIdleConn = info.WasIdle
		},
		WroteHeaders: func() {
			trace.mtx.Lock()
			defer trace.mtx.Unlock()
			if trace.final {
				return
			}
			trace.HeadersWritten = time.Now()
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			trace.mtx.Lock()
			defer trace.mtx.Unlock()
			if trace.final {
				return
			}
			trace.BodyWritten = time.Now()
		},
		GotFirstResponseByte: func() {
			trace.mtx.Lock()
			defer trace.mtx.Unlock()
			if trace.final {
				return
			}
			trace.FirstByte = time.Now()
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), ct))
	return req, trace
}
