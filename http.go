package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/strowger/types"
	"github.com/inconshreveable/go-vhost"
)

type HTTPListener struct {
	Addr      string
	TLSAddr   string
	TLSConfig *tls.Config

	mtx      sync.RWMutex
	domains  map[string]*httpRoute
	services map[string]*httpService

	discoverd DiscoverdClient
	ds        DataStore

	listener    net.Listener
	tlsListener net.Listener
	closed      bool

	watchersMtx sync.RWMutex
	watchers    map[chan *strowger.Event]struct{}
}

type DiscoverdClient interface {
	NewServiceSet(string) (discoverd.ServiceSet, error)
}

func NewHTTPListener(addr, tlsAddr string, ds DataStore, discoverdc DiscoverdClient) *HTTPListener {
	return &HTTPListener{
		Addr:      addr,
		TLSAddr:   tlsAddr,
		ds:        ds,
		discoverd: discoverdc,
		domains:   make(map[string]*httpRoute),
		services:  make(map[string]*httpService),
		watchers:  make(map[chan *strowger.Event]struct{}),
	}
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

func (s *HTTPListener) Watch(ch chan *strowger.Event) {
	s.watchersMtx.Lock()
	s.watchers[ch] = struct{}{}
	s.watchersMtx.Unlock()
}

func (s *HTTPListener) Unwatch(ch chan *strowger.Event) {
	go func() {
		// drain channel so that we don't deadlock
		for _ = range ch {
		}
	}()
	s.watchersMtx.Lock()
	delete(s.watchers, ch)
	s.watchersMtx.Unlock()
}

func (s *HTTPListener) sendEvent(e *strowger.Event) {
	s.watchersMtx.RLock()
	defer s.watchersMtx.RUnlock()
	for ch := range s.watchers {
		ch <- e
	}
}

var ErrClosed = errors.New("strowger: listener has been closed")

func (s *HTTPListener) AddRoute(domain string, service string, cert, key string) error {
	s.mtx.RLock()
	if s.closed {
		s.mtx.RUnlock()
		return ErrClosed
	}
	s.mtx.RUnlock()

	return s.ds.Add(domain, &httpRoute{
		Domain:  domain,
		Service: service,
		TLSCert: cert,
		TLSKey:  key,
	})
}

func (s *HTTPListener) RemoveRoute(domain string) error {
	s.mtx.RLock()
	if s.closed {
		s.mtx.RUnlock()
		return ErrClosed
	}
	s.mtx.RUnlock()

	return s.ds.Remove(domain)
}

var ErrDomainExists = errors.New("strowger: domain exists")
var ErrNoSuchDomain = errors.New("strowger: domain does not exist")

type httpSyncHandler struct {
	l *HTTPListener
}

func (h *httpSyncHandler) Add(data []byte) error {
	r := &httpRoute{}
	if err := json.Unmarshal(data, r); err != nil {
		return err
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
	if _, ok := h.l.domains[r.Domain]; ok {
		return ErrDomainExists
	}

	service := h.l.services[r.Service]
	if service == nil {
		ss, err := h.l.discoverd.NewServiceSet(r.Service)
		if err != nil {
			return err
		}
		service = &httpService{name: r.Service, ss: ss}
		h.l.services[r.Service] = service
	}
	service.refs++
	r.service = service
	h.l.domains[r.Domain] = r

	log.Println("Adding domain", r.Domain)
	go h.l.sendEvent(&strowger.Event{Event: "add", Domain: r.Domain})
	return nil
}

func (h *httpSyncHandler) Remove(name string) error {
	h.l.mtx.Lock()
	defer h.l.mtx.Unlock()
	r, ok := h.l.domains[name]
	if !ok {
		return ErrNoSuchDomain
	}

	r.service.refs--
	if r.service.refs <= 0 {
		r.service.ss.Close()
		delete(h.l.services, r.service.name)
	}

	delete(h.l.domains, name)
	log.Println("Removing domain", name)
	go h.l.sendEvent(&strowger.Event{Event: "remove", Domain: name})
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
	log.Printf("Backend match: %#v\n", backend)
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
		log.Println("SNI host is:", host)

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

	// Decode the first request from the connection
	sc := httputil.NewServerConn(conn, nil)
	req, err := sc.Read()
	if err != nil {
		if err != httputil.ErrPersistEOF {
			// TODO: log error
		}
		return
	}

	// If we do not have a backend yet (unencrypted connection),
	// look at the host header to find one or 404 out.
	if r == nil {
		r = s.findRouteForHost(req.Host)
		if r == nil {
			fail(sc, req, 404, "Not Found")
			return
		}
	}

	r.service.handle(req, sc, isTLS)
}

// A domain served by a listener, associated TLS certs,
// and link to backend service set.
type httpRoute struct {
	Domain  string
	Service string
	TLSCert string
	TLSKey  string

	keypair *tls.Certificate
	service *httpService
}

// A service definition: name, and set of backends.
type httpService struct {
	name string
	ss   discoverd.ServiceSet
	refs int
}

func (s *httpService) getBackend() *httputil.ClientConn {
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
		return httputil.NewClientConn(backend, nil)
	}
	// TODO: log no backends found error
	return nil
}

func (s *httpService) handle(req *http.Request, sc *httputil.ServerConn, tls bool) {
	req.Header.Set("X-Request-Start", strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10))
	backend := s.getBackend()
	if backend == nil {
		log.Println("no backend found")
		fail(sc, req, 503, "Service Unavailable")
		return
	}
	defer backend.Close()

	for {
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

		if err := backend.Write(req); err != nil {
			log.Println("server write err:", err)
			return
		}
		res, err := backend.Read(req)
		if res != nil {
			if err := sc.Write(req, res); err != nil {
				if err != io.EOF && err != httputil.ErrPersistEOF {
					log.Println("client write err:", err)
					// TODO: log error
				}
				return
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
				close(done)
			}()
			clientR.WriteTo(serverW)
			<-done
			return
		}

		// TODO: http pipelining
		req, err = sc.Read()
		if err != nil {
			if err != io.EOF && err != httputil.ErrPersistEOF {
				log.Println("client read err:", err)
			}
			return
		}
		req.Header.Set("X-Request-Start", strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10))
	}
}

func shuffle(s []string) []string {
	for i := len(s) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		s[i], s[j] = s[j], s[i]
	}
	return s
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
