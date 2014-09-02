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
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/code.google.com/p/go.crypto/nacl/secretbox"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/inconshreveable/go-vhost"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/router/types"
	log "gopkg.in/inconshreveable/log15.v2"
)

var l = log.New("module", "http")

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
	l.SetHandler(log.MultiHandler(
		log.StreamHandler(os.Stderr, log.TerminalFormat())))

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

type httpSyncHandler struct {
	l *HTTPListener
}

func (h *httpSyncHandler) Set(data *router.Route) error {
	route := data.HTTPRoute()
	r := &httpRoute{
		Domain:  route.Domain,
		Service: route.Service,
		TLSCert: route.TLSCert,
		TLSKey:  route.TLSKey,
		Sticky:  route.Sticky,
	}

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
		service = &httpService{name: r.Service, ss: ss, cookieKey: h.l.cookieKey}
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
	var connlog = l.New("conn", &conn, "raddr", conn.RemoteAddr())
	defer func() {
		connlog.Debug("Closing client connection")
		conn.Close()
	}()
	connlog.Debug("New client connection", "tls", isTLS)

	var r *httpRoute

	// For TLS, use the SNI hello to determine the domain.
	// At this stage, if we don't find a match, we simply
	// close the connection down.
	if isTLS {
		connlog.Debug("Going to determine domain via SNI")

		// Parse out host via SNI first
		vhostConn, err := vhost.TLS(conn)
		if err != nil {
			connlog.Warn("Failed to decode TLS connection", err)
			return
		}
		host := vhostConn.Host()
		connlog.Debug("SNI host of the request found")

		// Find a backend for the key
		r = s.findRouteForHost(host)
		if r == nil {
			connlog.Debug("SNI domain is not configured in strowger", "host", host)
			return
		}
		if r.keypair == nil {
			connlog.Info("TLS client connection, but no certificate defined for SNI domain", "host", host)
			return
		}

		// Init a TLS decryptor
		tlscfg := &tls.Config{Certificates: []tls.Certificate{*r.keypair}}
		conn = tls.Server(vhostConn, tlscfg)
	}

	sc := httputil.NewServerConn(conn, nil)
	for {
		connlog.Debug("Reading next request from client")
		req, err := sc.Read()
		if err != nil {
			if err != io.EOF && err != httputil.ErrPersistEOF {
				connlog.Error("Error reading client request", "err", err)
			}
			return
		}

		if !isTLS {
			r = s.findRouteForHost(req.Host)
			if r == nil {
				connlog.Debug("Domain from Host header is not configured in strowger", "host", req.Host)
				fail(sc, req, 404, "Not Found")
				continue
			}
		}

		var reqlog = connlog.New("req", &req, "srv", r.service.name)

		req.RemoteAddr = conn.RemoteAddr().String()
		if r.service.handle(req, sc, isTLS, r.Sticky, reqlog) {
			return
		}
	}
}

// A domain served by a listener, associated TLS certs,
// and link to backend service set.
type httpRoute struct {
	Domain  string
	Service string
	TLSCert string
	TLSKey  string
	Sticky  bool

	keypair *tls.Certificate
	service *httpService
}

// A service definition: name, and set of backends.
type httpService struct {
	name string
	ss   discoverd.ServiceSet
	refs int

	cookieKey *[32]byte
}

func (s *httpService) getBackend() *httputil.ClientConn {
	backend, _ := s.connectBackend()
	return backend
}

func (s *httpService) connectBackend() (*httputil.ClientConn, string) {
	for _, addr := range shuffle(s.ss.Addrs()) {
		// TODO: set connection timeout
		l.Debug("Dialing the backend", "addr", addr)
		backend, err := net.Dial("tcp", addr)
		if err != nil {
			// TODO: limit number of backends tried
			// TODO: temporarily quarantine failing backends
			l.Error("Error dialing the backend", "err", err)
			continue
		}
		return httputil.NewClientConn(backend, nil), addr
	}
	return nil, ""
}

const stickyCookie = "_backend"

func (s *httpService) getNewBackendSticky() (*httputil.ClientConn, *http.Cookie) {
	backend, addr := s.connectBackend()
	if backend == nil {
		return nil, nil
	}

	var nonce [24]byte
	_, err := io.ReadFull(rand.Reader, nonce[:])
	if err != nil {
		panic(err)
	}
	out := make([]byte, len(nonce), len(nonce)+len(addr)+secretbox.Overhead)
	copy(out, nonce[:])
	out = secretbox.Seal(out, []byte(addr), &nonce, s.cookieKey)

	return backend, &http.Cookie{Name: stickyCookie, Value: base64.StdEncoding.EncodeToString(out), Path: "/"}
}

func (s *httpService) getBackendSticky(req *http.Request) (*httputil.ClientConn, *http.Cookie) {
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
	return httputil.NewClientConn(backend, nil), nil
}

func (s *httpService) handle(req *http.Request, sc *httputil.ServerConn, tls, sticky bool, reqlog log.Logger) (done bool) {
	reqlog.Debug("Begin processing request")
	defer func() { reqlog.Debug("Done processing request") }()

	req.Header.Set("X-Request-Start", strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10))
	req.Header.Set("X-Request-Id", random.UUID())

	reqlog.Debug("Select backend for service")
	var backend *httputil.ClientConn
	var stickyCookie *http.Cookie
	if sticky {
		backend, stickyCookie = s.getBackendSticky(req)
	} else {
		backend = s.getBackend()
	}
	if backend == nil {
		reqlog.Warn("No backend for this service is online")
		fail(sc, req, 503, "Service Unavailable")
		return
	}
	defer backend.Close()

	if req.Method != "GET" && req.Method != "POST" && req.Method != "HEAD" &&
		req.Method != "OPTIONS" && req.Method != "PUT" && req.Method != "DELETE" && req.Method != "TRACE" {
		fail(sc, req, 405, "Method not allowed")
		return
	}

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

	reqlog.Debug("Starting I/O for request")
	if err := backend.Write(req); err != nil {
		reqlog.Error("Error writing to backend", "err", err)
		// TODO: return error to client here
		return true
	}
	res, err := backend.Read(req)
	reqlog.Debug("Backend returned response")
	if res != nil {
		if stickyCookie != nil {
			res.Header.Add("Set-Cookie", stickyCookie.String())
		}
		if res.StatusCode == http.StatusSwitchingProtocols {
			res.Body = nil
		}
		if err := sc.Write(req, res); err != nil {
			if err != io.EOF && err != httputil.ErrPersistEOF {
				reqlog.Error("Error while writing response to client:", "err", err)
			}
			return true
		}
	}
	if err != nil {
		if err != io.EOF && err != httputil.ErrPersistEOF {
			reqlog.Error("Error while reading response from backend:", "err", err)
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
