package main

import (
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/discoverd/cache"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/flynn/flynn/pkg/tlsconfig"
	"github.com/flynn/flynn/router/proxy"
	"github.com/flynn/flynn/router/types"
	"golang.org/x/net/context"
	"golang.org/x/net/http2"
)

type HTTPListener struct {
	Watcher
	DataStoreReader

	Addr    string
	TLSAddr string

	mtx      sync.RWMutex
	domains  map[string]*node
	routes   map[string]*httpRoute
	services map[string]*service

	discoverd DiscoverdClient
	ds        DataStore
	wm        *WatchManager
	stopSync  func()

	listener    net.Listener
	tlsListener net.Listener
	closed      bool
	cookieKey   *[32]byte
	keypair     tls.Certificate

	preSync  func()
	postSync func(<-chan struct{})
}

type DiscoverdClient interface {
	Service(string) discoverd.Service
	AddService(string, *discoverd.ServiceConfig) error
}

func (s *HTTPListener) Close() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.closed {
		return nil
	}
	s.stopSync()
	for _, service := range s.services {
		service.sc.Close()
	}
	if s.listener != nil {
		s.listener.Close()
	}
	if s.tlsListener != nil {
		s.tlsListener.Close()
	}
	s.closed = true
	return nil
}

func (s *HTTPListener) Start() error {
	ctx := context.Background() // TODO(benburkert): make this an argument
	ctx, s.stopSync = context.WithCancel(ctx)

	if s.Watcher != nil {
		return errors.New("router: http listener already started")
	}
	if s.wm == nil {
		s.wm = NewWatchManager()
	}
	s.Watcher = s.wm

	if s.ds == nil {
		return errors.New("router: http listener missing data store")
	}
	s.DataStoreReader = s.ds

	s.routes = make(map[string]*httpRoute)
	s.domains = make(map[string]*node)
	s.services = make(map[string]*service)

	if s.cookieKey == nil {
		s.cookieKey = &[32]byte{}
	}

	if err := s.startSync(ctx); err != nil {
		s.Close()
		return err
	}

	if err := s.startListen(); err != nil {
		s.Close()
		return err
	}

	return nil
}

func (s *HTTPListener) startSync(ctx context.Context) error {
	errc := make(chan error)
	startc := s.doSync(ctx, errc)

	select {
	case err := <-errc:
		return err
	case <-startc:
		go s.runSync(ctx, errc)
		return nil
	}
}

func (s *HTTPListener) runSync(ctx context.Context, errc chan error) {
	err := <-errc

	for {
		if err == nil {
			return
		}
		log.Printf("router: sync error: %s", err)

		time.Sleep(2 * time.Second)

		if s.preSync != nil {
			s.preSync()
		}

		startc := s.doSync(ctx, errc)

		if s.postSync != nil {
			s.postSync(startc)
		}

		err = <-errc
	}
}

func (s *HTTPListener) doSync(ctx context.Context, errc chan<- error) <-chan struct{} {
	startc := make(chan struct{})

	go func() { errc <- s.ds.Sync(ctx, &httpSyncHandler{l: s}, startc) }()

	return startc
}

func (s *HTTPListener) startListen() error {
	if err := s.listenAndServe(); err != nil {
		return err
	}
	s.Addr = s.listener.Addr().String()

	if err := s.listenAndServeTLS(); err != nil {
		s.listener.Close()
		return err
	}
	s.TLSAddr = s.tlsListener.Addr().String()

	return nil
}

var ErrClosed = errors.New("router: listener has been closed")

func (s *HTTPListener) AddRoute(r *router.Route) error {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if s.closed {
		return ErrClosed
	}
	return s.ds.Add(r)
}

func (s *HTTPListener) UpdateRoute(r *router.Route) error {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if s.closed {
		return ErrClosed
	}
	return s.ds.Update(r)
}

func md5sum(data string) string {
	digest := md5.Sum([]byte(data))
	return hex.EncodeToString(digest[:])
}

func (s *HTTPListener) RemoveRoute(id string) error {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if s.closed {
		return ErrClosed
	}
	return s.ds.Remove(id)
}

func (s *HTTPListener) AddCert(cert *router.Certificate) error {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if s.closed {
		return ErrClosed
	}
	return s.ds.AddCert(cert)
}

func (s *HTTPListener) GetCert(id string) (*router.Certificate, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}
	return s.ds.GetCert(id)
}

func (s *HTTPListener) GetCertRoutes(id string) ([]*router.Route, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}
	return s.ds.ListCertRoutes(id)
}

func (s *HTTPListener) GetCerts() ([]*router.Certificate, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}
	return s.ds.ListCerts()
}

func (s *HTTPListener) RemoveCert(id string) error {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if s.closed {
		return ErrClosed
	}
	return s.ds.RemoveCert(id)
}

type httpSyncHandler struct {
	l *HTTPListener
}

func (h *httpSyncHandler) Current() map[string]struct{} {
	h.l.mtx.RLock()
	defer h.l.mtx.RUnlock()
	ids := make(map[string]struct{}, len(h.l.routes))
	for id := range h.l.routes {
		ids[id] = struct{}{}
	}
	return ids
}

func (h *httpSyncHandler) Set(data *router.Route) error {
	route := data.HTTPRoute()
	r := &httpRoute{HTTPRoute: route}
	cert := r.Certificate

	if cert != nil && cert.Cert != "" && cert.Key != "" {
		kp, err := tls.X509KeyPair([]byte(cert.Cert), []byte(cert.Key))
		if err != nil {
			return err
		}
		r.keypair = &kp
		r.Certificate = nil
	}

	h.l.mtx.Lock()
	defer h.l.mtx.Unlock()
	if h.l.closed {
		return nil
	}

	service := h.l.services[r.Service]
	if service != nil && service.name != r.Service {
		service.refs--
		if service.refs <= 0 {
			service.Close()
			delete(h.l.services, service.name)
		}
		service = nil
	}
	if service == nil {
		sc, err := cache.New(h.l.discoverd.Service(r.Service))
		if err != nil {
			return err
		}

		service = newService(r.Service, sc, h.l.wm, r.DrainBackends)
		h.l.services[r.Service] = service
	}
	service.refs++
	var bf proxy.BackendListFunc
	if r.Leader {
		bf = service.sc.LeaderAddr
	} else {
		bf = service.sc.Addrs
	}
	r.rp = proxy.NewReverseProxy(bf, h.l.cookieKey, r.Sticky, service, logger)
	r.service = service
	h.l.routes[data.ID] = r
	if data.Path == "/" {
		if tree, ok := h.l.domains[strings.ToLower(r.Domain)]; ok {
			tree.backend = r
		} else {
			h.l.domains[strings.ToLower(r.Domain)] = NewTree(r)
		}
	} else {
		if tree, ok := h.l.domains[strings.ToLower(r.Domain)]; ok {
			tree.Insert(r.Path, r)
		} else {
			logger.Error("Failed insert of path based route, consistency violation.")
		}
	}

	go h.l.wm.Send(&router.Event{Event: router.EventTypeRouteSet, ID: r.Domain, Route: r.ToRoute()})
	return nil
}

func (h *httpSyncHandler) Remove(id string) error {
	h.l.mtx.Lock()
	defer h.l.mtx.Unlock()
	if h.l.closed {
		return nil
	}
	r, ok := h.l.routes[id]
	if !ok {
		return ErrNotFound
	}

	r.service.refs--
	if r.service.refs <= 0 {
		r.service.sc.Close()
		delete(h.l.services, r.service.name)
	}

	delete(h.l.routes, id)
	if tree, ok := h.l.domains[r.Domain]; ok {
		if r.Path == "/" && tree.backend == r {
			delete(h.l.domains, r.Domain)
		} else if tree.Lookup(r.Path) == r {
			tree.Remove(r.Path)
		}
	}
	go h.l.wm.Send(&router.Event{Event: router.EventTypeRouteRemove, ID: id, Route: r.ToRoute()})
	return nil
}

func (s *HTTPListener) listenAndServe() error {
	var err error
	s.listener, err = listenFunc("tcp4", s.Addr)
	if err != nil {
		return listenErr{s.Addr, err}
	}

	server := &http.Server{
		Addr: s.listener.Addr().String(),
		Handler: fwdProtoHandler{
			Handler: s,
			Proto:   "http",
			Port:    mustPortFromAddr(s.listener.Addr().String()),
		},
	}

	// TODO: log error
	go server.Serve(s.listener)
	return nil
}

var errMissingTLS = errors.New("router: route not found or TLS not configured")

func (s *HTTPListener) listenAndServeTLS() error {
	certForHandshake := func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		r := s.findRoute(hello.ServerName, "/")
		if r == nil {
			return nil, errMissingTLS
		}
		return r.keypair, nil
	}
	tlsConfig := tlsconfig.SecureCiphers(&tls.Config{
		GetCertificate: certForHandshake,
		Certificates:   []tls.Certificate{s.keypair},
		NextProtos:     []string{http2.NextProtoTLS, "h2-14"},
	})

	l, err := listenFunc("tcp4", s.TLSAddr)
	if err != nil {
		return listenErr{s.Addr, err}
	}
	s.tlsListener = tls.NewListener(l, tlsConfig)

	handler := fwdProtoHandler{
		Handler: s,
		Proto:   "https",
		Port:    mustPortFromAddr(s.tlsListener.Addr().String()),
	}
	http2Server := &http2.Server{}
	http2Handler := func(hs *http.Server, c *tls.Conn, h http.Handler) {
		http2Server.ServeConn(c, &http2.ServeConnOpts{
			Handler:    handler,
			BaseConfig: hs,
		})
	}

	server := &http.Server{
		Addr:    s.tlsListener.Addr().String(),
		Handler: handler,
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){
			http2.NextProtoTLS: http2Handler,
			"h2-14":            http2Handler,
		},
	}

	// TODO: log error
	go server.Serve(s.tlsListener)
	return nil
}

func (s *HTTPListener) findRoute(host string, path string) *httpRoute {
	host = strings.ToLower(host)
	if strings.Contains(host, ":") {
		host, _, _ = net.SplitHostPort(host)
	}
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if tree, ok := s.domains[host]; ok {
		return tree.Lookup(path)
	}
	// handle wildcard domains up to 5 subdomains deep, from most-specific to
	// least-specific
	d := strings.SplitN(host, ".", 5)
	for i := len(d); i > 0; i-- {
		if tree, ok := s.domains["*."+strings.Join(d[len(d)-i:], ".")]; ok {
			return tree.Lookup(path)
		}
	}
	return nil
}

func fail(w http.ResponseWriter, code int) {
	msg := []byte(http.StatusText(code) + "\n")
	w.Header().Set("Content-Length", strconv.Itoa(len(msg)))
	w.WriteHeader(code)
	w.Write(msg)
}

func (s *HTTPListener) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := context.Background()
	ctx = ctxhelper.NewContextStartTime(ctx, time.Now())
	r := s.findRoute(req.Host, req.URL.Path)
	if r == nil {
		fail(w, 404)
		return
	}

	r.ServeHTTP(ctx, w, req)
}

// A domain served by a listener, associated TLS certs,
// and link to backend service set.
type httpRoute struct {
	*router.HTTPRoute

	keypair *tls.Certificate
	service *service
	rp      *proxy.ReverseProxy
}

// A service definition: name, and set of backends.
type service struct {
	name   string
	sc     *cache.ServiceCache
	refs   int
	wm     *WatchManager
	stream stream.Stream
	reqs   map[string]int64
	cond   *sync.Cond
}

func newService(name string, sc *cache.ServiceCache, wm *WatchManager, trackBackends bool) *service {
	s := &service{
		name: name,
		sc:   sc,
		wm:   wm,
	}
	if trackBackends {
		events := make(chan *discoverd.Event)
		s.stream = sc.Watch(events, true)
		s.reqs = make(map[string]int64)
		s.cond = sync.NewCond(&sync.Mutex{})
		go s.watchBackends(events)
	}
	return s
}

func (s *service) TrackRequestStart(backend string) {
	if s.reqs == nil {
		return
	}
	s.cond.L.Lock()
	s.reqs[backend]++
	s.cond.L.Unlock()
}

func (s *service) TrackRequestDone(backend string) {
	if s.reqs == nil {
		return
	}
	s.cond.L.Lock()
	s.reqs[backend]--
	if s.reqs[backend] == 0 {
		s.cond.Broadcast()
	}
	s.cond.L.Unlock()
}

func (s *service) Close() {
	if s.stream != nil {
		s.stream.Close()
	}
	s.sc.Close()
}

func (s *service) watchBackends(events chan *discoverd.Event) {
	for event := range events {
		go s.handleBackendEvent(event)
	}
}

func (s *service) handleBackendEvent(event *discoverd.Event) {
	if event.Instance == nil {
		return
	}
	backend := &router.Backend{
		Service: s.name,
		Addr:    event.Instance.Addr,
		JobID:   event.Instance.Meta["FLYNN_JOB_ID"],
	}
	switch event.Kind {
	case discoverd.EventKindUp:
		s.wm.Send(&router.Event{
			Event:   router.EventTypeBackendUp,
			Backend: backend,
		})
	case discoverd.EventKindDown:
		s.wm.Send(&router.Event{
			Event:   router.EventTypeBackendDown,
			Backend: backend,
		})

		// wait for in-flight requests to finish then send a
		// drained event
		s.cond.L.Lock()
		for s.reqs[backend.Addr] > 0 {
			s.cond.Wait()
		}
		s.cond.L.Unlock()
		s.wm.Send(&router.Event{
			Event:   router.EventTypeBackendDrained,
			Backend: backend,
		})
	}
}

func (r *httpRoute) ServeHTTP(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	start, _ := ctxhelper.StartTimeFromContext(ctx)
	req.Header.Set("X-Request-Start", strconv.FormatInt(start.UnixNano()/int64(time.Millisecond), 10))
	req.Header.Set("X-Request-Id", random.UUID())

	r.rp.ServeHTTP(ctx, w, req)
}

func mustPortFromAddr(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		panic(err)
	}
	return port
}
