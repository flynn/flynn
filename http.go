package main

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-etcd/etcd"
	"github.com/inconshreveable/go-vhost"
)

type HTTPListener struct {
	Addr      string
	TLSAddr   string
	TLSConfig *tls.Config

	mtx      sync.RWMutex
	domains  map[string]*route
	services map[string]*httpService

	etcdPrefix string

	etcd      EtcdClient
	discoverd DiscoverdClient

	listener    net.Listener
	tlsListener net.Listener
	stopSync    chan bool
	closed      bool
}

type EtcdClient interface {
	Create(key string, value string, ttl uint64) (*etcd.Response, error)
	Get(key string, sort, recursive bool) (*etcd.Response, error)
	Delete(key string, recursive bool) (*etcd.Response, error)
	Watch(prefix string, waitIndex uint64, recursive bool, receiver chan *etcd.Response, stop chan bool) (*etcd.Response, error)
}

type DiscoverdClient interface {
	NewServiceSet(string) (discoverd.ServiceSet, error)
}

func NewHTTPListener(addr, tlsAddr string, etcdc EtcdClient, discoverdc DiscoverdClient) *HTTPListener {
	return &HTTPListener{
		Addr:       addr,
		TLSAddr:    tlsAddr,
		etcd:       etcdc,
		etcdPrefix: "/strowger/http/",
		discoverd:  discoverdc,
		domains:    make(map[string]*route),
		services:   make(map[string]*httpService),
		stopSync:   make(chan bool),
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
	close(s.stopSync)
	s.closed = true
	return nil
}

func (s *HTTPListener) Start() error {
	started := make(chan error)

	go s.syncDatabase(started)
	if err := <-started; err != nil {
		return err
	}

	go s.serve(started)
	if err := <-started; err != nil {
		close(s.stopSync)
		return err
	}
	s.Addr = s.listener.Addr().String()

	go s.serveTLS(started)
	if err := <-started; err != nil {
		close(s.stopSync)
		s.listener.Close()
		return err
	}
	s.TLSAddr = s.tlsListener.Addr().String()

	return nil
}

var ErrClosed = errors.New("strowger: listener has been closed")

func (s *HTTPListener) AddHTTPDomain(domain string, service string, cert []byte, key []byte) error {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if s.closed {
		return ErrClosed
	}

	var err error
	if _, err = s.etcd.Create(s.etcdPrefix+domain+"/service", service, 0); err != nil {
		goto error
	}
	if len(key) > 0 && len(cert) > 0 {
		if _, err = s.etcd.Create(s.etcdPrefix+domain+"/tls/key", string(key), 0); err != nil {
			goto error
		}
		if _, err = s.etcd.Create(s.etcdPrefix+domain+"/tls/cert", string(cert), 0); err != nil {
			goto error
		}
	}
	return nil
error:
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 105 {
		return ErrDomainExists
	}
	return err
}

func (s *HTTPListener) RemoveHTTPDomain(domain string) error {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if s.closed {
		return ErrClosed
	}

	if _, err := s.etcd.Delete(s.etcdPrefix+domain, true); err != nil {
		if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
			return ErrNoSuchDomain
		}
		return err
	}
	return nil
}

var ErrDomainExists = errors.New("strowger: domain exists")
var ErrNoSuchDomain = errors.New("strowger: domain does not exist")

func (s *HTTPListener) addDomain(name string) (*route, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if d, ok := s.domains[name]; ok {
		return d, ErrDomainExists
	}

	r := &route{domain: name}
	s.domains[name] = r
	log.Println("Adding domain", r.domain)
	return r, nil
}

func (s *HTTPListener) removeDomain(name string) error {
	s.mtx.Lock()
	d, ok := s.domains[name]
	if !ok {
		s.mtx.Unlock()
		return ErrNoSuchDomain
	}
	s.mtx.Unlock()

	err := s.setDomainService(d, "")
	if err != nil {
		return err
	}

	s.mtx.Lock()
	defer s.mtx.Unlock()
	delete(s.domains, name)
	log.Println("Removing domain", name)
	return nil
}

func (s *HTTPListener) setDomainService(r *route, serviceName string) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if r.service != nil {
		r.service.refs--
		if r.service.refs <= 0 {
			err := r.service.ss.Close()
			if err != nil {
				log.Println("Error closing the discoverd connection for ", r.service.name, err)
			}
			delete(s.services, r.service.name)
		}
	}

	var service *httpService
	if serviceName != "" {
		service = s.services[serviceName]
		if service == nil {
			ss, err := s.discoverd.NewServiceSet(serviceName)
			if err != nil {
				return err
			}
			service = &httpService{name: serviceName, ss: ss}
		}

		service.refs++
		s.services[serviceName] = service
	}

	r.service = service
	log.Println("Setting service of domain", r.domain, "to", service)
	return nil
}

// Set either of or both cert and key. The TLS config is complete when both have
// been provided. Pass nil to indicate the value should not change. Pass an empty
// byte array to clear the value.
func (s *HTTPListener) setDomainTLSConfig(r *route, cert []byte, key []byte) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if cert != nil {
		r.cert = cert
	}
	if key != nil {
		r.key = key
	}

	// Once both key and cert have been set, parse and validate
	if len(r.cert) > 0 && len(r.key) > 0 {
		var keypair *tls.Certificate
		if cert != nil && key != nil {
			created, err := tls.X509KeyPair(cert, key)
			if err != nil {
				return err
			}
			keypair = &created
		}
		r.keypair = keypair
		log.Println("Setting SSL config of domain", r.domain)
	} else {
		if r.keypair != nil {
			r.keypair = nil
			log.Println("Removing SSL config of domain", r.domain)
		}
	}
	return nil
}

// Given a recursive node structure from etcd, find particular
// values. "name" may contain slashes ("foo/bar")
func etcdFindChild(node *etcd.Node, name string) *etcd.Node {
	parts := strings.Split(name, "/")
outer:
	for _, part := range parts {
		for _, childNode := range node.Nodes {
			if path.Base(childNode.Key) == part {
				node = childNode
				continue outer
			}
		}
		return nil
	}

	return node
}

func (s *HTTPListener) syncDatabase(started chan<- error) {
	var since uint64
	data, err := s.etcd.Get(s.etcdPrefix, false, true)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
		// key not found, ignore
		goto watch
	}
	if err != nil {
		started <- err
		return
	}
	since = data.EtcdIndex
	for _, node := range data.Node.Nodes {
		if !node.Dir {
			continue
		}
		domain := path.Base(node.Key)

		d, err := s.addDomain(domain)
		if err != nil {
			started <- err
			return
		}

		// Find the individual (service name, ssl info)
		serviceNode := etcdFindChild(node, "service")
		if serviceNode != nil {
			err = s.setDomainService(d, serviceNode.Value)
			if err != nil {
				started <- err
				return
			}
		}

		var cert, key []byte
		certNode := etcdFindChild(node, "tls/cert")
		if certNode != nil && certNode.Value != "" {
			cert = []byte(certNode.Value)
		}
		keyNode := etcdFindChild(node, "tls/key")
		if keyNode != nil && keyNode.Value != "" {
			key = []byte(keyNode.Value)
		}
		err = s.setDomainTLSConfig(d, cert, key)
		if err != nil {
			started <- err
			return
		}
	}

watch:
	started <- nil
	stream := make(chan *etcd.Response)
	go s.etcd.Watch(s.etcdPrefix, since, true, stream, s.stopSync)
	for res := range stream {
		keyRelToBase := res.Node.Key[len(s.etcdPrefix):]
		parts := strings.SplitN(keyRelToBase, "/", 2)
		domain := parts[0]
		var key string
		if len(parts) == 2 {
			key = parts[1]
		}

		var err error
		if res.Action == "delete" && res.Node.Dir && key == "" {
			// Directory for a domain was removed
			err = s.removeDomain(domain)
		} else {
			value := res.Node.Value
			if res.Action == "delete" {
				value = ""
			}

			d, _ := s.addDomain(domain)
			switch key {
			case "service":
				err = s.setDomainService(d, value)
			case "tls/key":
				err = s.setDomainTLSConfig(d, nil, []byte(value))
			case "tls/cert":
				err = s.setDomainTLSConfig(d, []byte(value), nil)
			}
		}

		if err != nil {
			panic(fmt.Sprintf("Error while processing update from etcd: %s", err))
		}
	}
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

func (s *HTTPListener) findRouteForHost(host string) *route {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	// TODO: handle wildcard domains
	log.Println(s.domains, host)
	backend := s.domains[host]
	log.Println("Backend match:", backend)
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

	var r *route

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
		log.Println(req, r)
		if r == nil {
			fail(sc, req, 404, "Not Found")
			return
		}
	}

	if r.service == nil {
		log.Println("Matching service is not configured yet")
		fail(sc, req, 404, "Not Found")
		return
	}

	r.service.handle(req, sc, isTLS)
}

// A domain served by a listener, associated TLS certs,
// and link to backend service set.
type route struct {
	domain  string
	service *httpService
	// Keypair created from cert/byte when both are available
	cert    []byte
	key     []byte
	keypair *tls.Certificate
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
