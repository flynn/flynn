package main

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/code.google.com/p/go.crypto/nacl/secretbox"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/inconshreveable/go-vhost"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/router/types"
)

type HTTPListener struct {
	Watcher
	DataStoreReader

	Addr      string
	TLSAddr   string
	TLSConfig *tls.Config

	mtx      sync.RWMutex
	domains  map[string]*httpRoute
	routes   map[string]*httpRoute
	services map[string]*httpService

	discoverd DiscoverdClient
	ds        DataStore
	wm        *WatchManager

	listener    net.Listener
	tlsListener net.Listener
	closed      bool
	cookieKey   *[32]byte
}

type DiscoverdClient interface {
	NewServiceSet(string) (discoverd.ServiceSet, error)
}

func NewHTTPListener(addr, tlsAddr string, cookieKey *[32]byte, ds DataStore, discoverdc DiscoverdClient) *HTTPListener {
	l := &HTTPListener{
		Addr:      addr,
		TLSAddr:   tlsAddr,
		ds:        ds,
		discoverd: discoverdc,
		routes:    make(map[string]*httpRoute),
		domains:   make(map[string]*httpRoute),
		services:  make(map[string]*httpService),
		wm:        NewWatchManager(),
		cookieKey: cookieKey,
	}
	if cookieKey == nil {
		var k [32]byte
		l.cookieKey = &k
	}
	l.Watcher = l.wm
	l.DataStoreReader = l.ds
	return l
}

func (s *HTTPListener) Close() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	for _, service := range s.services {
		service.ss.Close()
	}
	s.listener.Close()
	s.tlsListener.Close()
	s.ds.StopSync()
	s.closed = true
	return nil
}

func (s *HTTPListener) Start() error {
	started := make(chan error)

	go s.ds.Sync(&httpSyncHandler{l: s}, started)
	if err := <-started; err != nil {
		return err
	}

	go s.serve(started)
	if err := <-started; err != nil {
		s.ds.StopSync()
		return err
	}
	s.Addr = s.listener.Addr().String()

	go s.serveTLS(started)
	if err := <-started; err != nil {
		s.ds.StopSync()
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
	r.ID = md5sum(r.HTTPRoute().Domain)
	return s.ds.Add(r)
}

func (s *HTTPListener) SetRoute(r *router.Route) error {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if s.closed {
		return ErrClosed
	}
	r.ID = md5sum(r.HTTPRoute().Domain)
	return s.ds.Set(r)
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

func (s *HTTPListener) PauseService(name string, pause bool) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.closed {
		return ErrClosed
	}
	service, ok := s.services[name]
	if !ok {
		return nil
	}
	if pause {
		service.Pause()
	} else {
		service.Unpause()
	}
	return nil
}

func (s *HTTPListener) AddDrainListener(name string, ch chan string) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	srv := s.services[name]
	srv.listenerMtx.Lock()
	srv.listeners[ch] = struct{}{}
	srv.listenerMtx.Unlock()
}

func (s *HTTPListener) RemoveDrainListener(name string, ch chan string) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	srv := s.services[name]
	srv.listenerMtx.Lock()
	delete(srv.listeners, ch)
	srv.listenerMtx.Unlock()
}

type httpSyncHandler struct {
	l *HTTPListener
}

func (h *httpSyncHandler) Set(data *router.Route) error {
	route := data.HTTPRoute()
	r := &httpRoute{HTTPRoute: route}

	if r.TLSCert != "" && r.TLSKey != "" {
		kp, err := tls.X509KeyPair([]byte(r.TLSCert), []byte(r.TLSKey))
		if err != nil {
			return err
		}
		r.keypair = &kp
		r.TLSCert = ""
		r.TLSKey = ""
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
			service.ss.Close()
			delete(h.l.services, service.name)
		}
		service = nil
	}
	if service == nil {
		ss, err := h.l.discoverd.NewServiceSet(r.Service)
		if err != nil {
			return err
		}
		service = &httpService{
			name:      r.Service,
			ss:        ss,
			cookieKey: h.l.cookieKey,
			requests:  make(map[string]int32),
			listeners: make(map[chan string]interface{}),
			paused:    false,
		}
		service.resumeCond = sync.NewCond(service.pauseMtx.RLocker())
		h.l.services[r.Service] = service
	}
	service.refs++
	r.service = service
	h.l.routes[data.ID] = r
	h.l.domains[r.Domain] = r

	go h.l.wm.Send(&router.Event{Event: "set", ID: r.Domain})
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
		r.service.ss.Close()
		delete(h.l.services, r.service.name)
	}

	delete(h.l.routes, id)
	delete(h.l.domains, r.Domain)
	go h.l.wm.Send(&router.Event{Event: "remove", ID: id})
	return nil
}

func (s *HTTPListener) serve(started chan<- error) {
	var err error
	s.listener, err = net.Listen("tcp", s.Addr)
	started <- err
	if err != nil {
		return
	}
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// TODO: log error
			break
		}
		go s.handle(conn, false)
	}
}

func (s *HTTPListener) serveTLS(started chan<- error) {
	var err error
	s.tlsListener, err = net.Listen("tcp", s.TLSAddr)
	started <- err
	if err != nil {
		return
	}
	for {
		conn, err := s.tlsListener.Accept()
		if err != nil {
			// TODO: log error
			break
		}
		go s.handle(conn, true)
	}
}

func (s *HTTPListener) findRouteForHost(host string) *httpRoute {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	// TODO: handle wildcard domains
	backend := s.domains[host]
	return backend
}

func fail(sc *httputil.ServerConn, req *http.Request, code int, msg string) {
	resp := &http.Response{
		StatusCode:    code,
		ProtoMajor:    1,
		ProtoMinor:    0,
		Request:       req,
		Body:          ioutil.NopCloser(bytes.NewBufferString(msg)),
		ContentLength: int64(len(msg)),
	}
	sc.Write(req, resp)
}

func (s *HTTPListener) handle(conn net.Conn, isTLS bool) {
	defer conn.Close()

	var r *httpRoute

	// For TLS, use the SNI hello to determine the domain.
	// At this stage, if we don't find a match, we simply
	// close the connection down.
	if isTLS {
		// Parse out host via SNI first
		vhostConn, err := vhost.TLS(conn)
		if err != nil {
			log.Println("Failed to decode TLS connection", err)
			return
		}
		host := vhostConn.Host()

		// Find a backend for the key
		r = s.findRouteForHost(host)
		if r == nil {
			return
		}
		if r.keypair == nil {
			log.Println("Cannot serve TLS, no certificate defined for this domain")
			return
		}

		// Init a TLS decryptor
		tlscfg := &tls.Config{Certificates: []tls.Certificate{*r.keypair}}
		conn = tls.Server(vhostConn, tlscfg)
	}

	sc := httputil.NewServerConn(conn, nil)
	for {
		req, err := sc.Read()
		if err != nil {
			if err != io.EOF && err != httputil.ErrPersistEOF {
				log.Println("client read err:", err)
			}
			return
		}

		if !isTLS {
			r = s.findRouteForHost(req.Host)
			if r == nil {
				fail(sc, req, 404, "Not Found")
				continue
			}
		}

		req.RemoteAddr = conn.RemoteAddr().String()
		if r.service.handle(req, sc, isTLS, r.Sticky) {
			return
		}
	}
}

// A domain served by a listener, associated TLS certs,
// and link to backend service set.
type httpRoute struct {
	*router.HTTPRoute

	keypair *tls.Certificate
	service *httpService
}

// A service definition: name, and set of backends.
type httpService struct {
	name string
	ss   discoverd.ServiceSet
	refs int

	cookieKey   *[32]byte
	requests    map[string]int32
	requestMtx  sync.RWMutex
	listeners   map[chan string]interface{}
	listenerMtx sync.RWMutex

	paused     bool
	pauseMtx   sync.RWMutex
	resumeCond *sync.Cond
}

func (s *httpService) Pause() {
	s.pauseMtx.Lock()
	s.paused = true
	s.pauseMtx.Unlock()
}

func (s *httpService) Unpause() {
	s.pauseMtx.Lock()
	s.paused = false
	s.pauseMtx.Unlock()
	s.resumeCond.Broadcast()
}

func (s *httpService) sendEvent(event string) {
	s.listenerMtx.RLock()
	defer s.listenerMtx.RUnlock()
	for ch := range s.listeners {
		ch <- event
	}
}

func (s *httpService) getBackend() (*httputil.ClientConn, string) {
	backend, addr := s.connectBackend()
	return backend, addr
}

func (s *httpService) connectBackend() (*httputil.ClientConn, string) {
	for _, addr := range shuffle(s.ss.Addrs()) {
		// TODO: set connection timeout
		backend, err := net.Dial("tcp", addr)
		if err != nil {
			// TODO: log error
			// TODO: limit number of backends tried
			// TODO: temporarily quarantine failing backends
			log.Println("backend error", err)
			continue
		}
		return httputil.NewClientConn(backend, nil), addr
	}
	// TODO: log no backends found error
	return nil, ""
}

const stickyCookie = "_backend"

func (s *httpService) getNewBackendSticky() (*httputil.ClientConn, string, *http.Cookie) {
	backend, addr := s.connectBackend()
	if backend == nil {
		return nil, "", nil
	}

	var nonce [24]byte
	_, err := io.ReadFull(rand.Reader, nonce[:])
	if err != nil {
		panic(err)
	}
	out := make([]byte, len(nonce), len(nonce)+len(addr)+secretbox.Overhead)
	copy(out, nonce[:])
	out = secretbox.Seal(out, []byte(addr), &nonce, s.cookieKey)

	return backend, addr, &http.Cookie{Name: stickyCookie, Value: base64.StdEncoding.EncodeToString(out), Path: "/"}
}

func (s *httpService) getBackendSticky(req *http.Request) (*httputil.ClientConn, string, *http.Cookie) {
	cookie, err := req.Cookie(stickyCookie)
	if err != nil {
		return s.getNewBackendSticky()
	}

	data, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		return s.getNewBackendSticky()
	}
	var nonce [24]byte
	if len(data) < len(nonce) {
		return s.getNewBackendSticky()
	}
	copy(nonce[:], data)
	res, ok := secretbox.Open(nil, data[len(nonce):], &nonce, s.cookieKey)
	if !ok {
		return s.getNewBackendSticky()
	}

	addr := string(res)
	ok = false
	for _, a := range s.ss.Addrs() {
		if a == addr {
			ok = true
			break
		}
	}
	if !ok {
		return s.getNewBackendSticky()
	}

	backend, err := net.Dial("tcp", string(addr))
	if err != nil {
		return s.getNewBackendSticky()
	}
	return httputil.NewClientConn(backend, nil), addr, nil
}

func (s *httpService) handle(req *http.Request, sc *httputil.ServerConn, tls, sticky bool) (done bool) {
	req.Header.Set("X-Request-Start", strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10))
	req.Header.Set("X-Request-Id", random.UUID())

	s.pauseMtx.RLock()
	if s.paused {
		s.resumeCond.Wait()
	}
	s.pauseMtx.RUnlock()

	var backend *httputil.ClientConn
	var addr string
	var stickyCookie *http.Cookie
	if sticky {
		backend, addr, stickyCookie = s.getBackendSticky(req)
	} else {
		backend, addr = s.getBackend()
	}
	if backend == nil {
		log.Println("no backend found")
		fail(sc, req, 503, "Service Unavailable")
		return
	}
	defer backend.Close()

	s.requestMtx.Lock()
	s.requests[addr]++
	s.requestMtx.Unlock()

	req.Proto = "HTTP/1.1"
	req.ProtoMajor = 1
	req.ProtoMinor = 1
	delete(req.Header, "Te")
	delete(req.Header, "Transfer-Encoding")

	if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		// If we aren't the first proxy retain prior
		// X-Forwarded-For information as a comma+space
		// separated list and fold multiple headers into one.
		if prior, ok := req.Header["X-Forwarded-For"]; ok {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		req.Header.Set("X-Forwarded-For", clientIP)
	}
	if tls {
		req.Header.Set("X-Forwarded-Proto", "https")
	} else {
		req.Header.Set("X-Forwarded-Proto", "http")
	}
	// TODO: Set X-Forwarded-Port

	// Pass the Request-URI verbatim without any modifications
	req.URL.Opaque = strings.Split(strings.TrimPrefix(req.RequestURI, req.URL.Scheme+":"), "?")[0]

	if err := backend.Write(req); err != nil {
		log.Println("server write err:", err)
		// TODO: return error to client here
		return true
	}
	res, err := backend.Read(req)
	if res != nil {
		if stickyCookie != nil {
			res.Header.Add("Set-Cookie", stickyCookie.String())
		}
		if res.StatusCode == http.StatusSwitchingProtocols {
			res.Body = nil
		}
		if err := sc.Write(req, res); err != nil {
			if err != io.EOF && err != httputil.ErrPersistEOF {
				log.Println("client write err:", err)
				// TODO: log error
			}
			return true
		}
	}
	if err != nil {
		if err != io.EOF && err != httputil.ErrPersistEOF {
			log.Println("server read err:", err)
			// TODO: log error
			fail(sc, req, 502, "Bad Gateway")
		}
		return
	}

	// TODO: Proxy HTTP CONNECT? (example: Go RPC over HTTP)
	if res.StatusCode == http.StatusSwitchingProtocols {
		serverW, serverR := backend.Hijack()
		clientW, clientR := sc.Hijack()
		defer serverW.Close()
		done := make(chan struct{})
		go func() {
			serverR.WriteTo(clientW)
			if cw, ok := clientW.(writeCloser); ok {
				cw.CloseWrite()
			}
			close(done)
		}()
		clientR.WriteTo(serverW)
		serverW.(writeCloser).CloseWrite()
		<-done
		return true
	}

	s.requestMtx.Lock()
	s.requests[addr]--
	if s.requests[addr] == 0 {
		delete(s.requests, addr)
		s.sendEvent(addr)
	}
	if len(s.requests) == 0 {
		s.sendEvent("all")
	}
	s.requestMtx.Unlock()

	return
}

type writeCloser interface {
	CloseWrite() error
}

func shuffle(s []string) []string {
	for i := len(s) - 1; i > 0; i-- {
		j := random.Math.Intn(i + 1)
		s[i], s[j] = s[j], s[i]
	}
	return s
}
