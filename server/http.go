package server

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flynn/go-discover/discover"
)

type HTTPFrontend struct {
	Addr      string
	TLSAddr   string
	TLSConfig *tls.Config

	mtx      sync.RWMutex
	domains  map[string]*httpServer
	services map[string]*httpServer
}

func (s *HTTPFrontend) AddDomain(domain string, service string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	server := s.services[service]
	if server == nil {
		// TODO: connect to service discovery
		server = &httpServer{name: service}
		s.services[service] = server
	}
	server.refs++
	s.domains[domain] = server
	// TODO: persist
}

func (s *HTTPFrontend) RemoveDomain(domain string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	server := s.domains[domain]
	if server == nil {
		return
	}
	delete(s.domains, domain)
	server.refs++
	if server.refs <= 0 {
		// TODO: close service set stream
		delete(s.services, server.name)
	}
	// TODO: persist
}

func (s *HTTPFrontend) serve() {
	l, err := net.Listen("tcp", s.Addr)
	if err != nil {
		// TODO: log error
		return
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			// TODO: log error
			break
		}
		go s.handle(conn)
	}
}

func (s *HTTPFrontend) serveTLS() {
	l, err := net.Listen("tcp", s.TLSAddr)
	if err != nil {
		// TODO: log error
		return
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			// TODO: log error
			break
		}
		go s.handle(conn)
	}
}

func (s *HTTPFrontend) handle(conn net.Conn) {
	defer conn.Close()
	sc := httputil.NewServerConn(conn, nil)
	req, err := sc.Read()
	if err != nil {
		if err != httputil.ErrPersistEOF {
			// TODO: log error
		}
		return
	}

	s.mtx.RLock()
	// TODO: handle wildcard domains
	backend := s.domains[req.Header.Get("Host")]
	s.mtx.RUnlock()
	if backend == nil {
		// TODO: return 404
	}
	_, tls := conn.(*tls.Conn)
	backend.handle(req, sc, tls)
}

type httpServer struct {
	name     string
	services *discover.ServiceSet
	refs     int
}

func (s *httpServer) getBackend() *httputil.ClientConn {
	for _, addr := range s.services.OnlineAddrs() {
		// TODO: set connection timeout
		backend, err := net.Dial("tcp", addr)
		if err != nil {
			// TODO: log error
			// TODO: limit number of backends tried
			// TODO: temporarily quarantine failing backends
			continue
		}
		return httputil.NewClientConn(backend, nil)
	}
	// TODO: log no backends found error
	return nil
}

func (s *httpServer) handle(req *http.Request, sc *httputil.ServerConn, tls bool) {
	req.Header.Set("X-Request-Start", strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10))
	backend := s.getBackend()
	if backend == nil {
		// TODO: Return 503
		return
	}
	defer backend.Close()

	for {
		if req.Method != "GET" && req.Method != "POST" && req.Method != "HEAD" &&
			req.Method != "OPTIONS" && req.Method != "PUT" && req.Method != "DELETE" && req.Method != "TRACE" {
			// TODO: return 405
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

		res, err := backend.Read(req)
		if res != nil {
			if err := sc.Write(req, res); err != nil {
				if err != httputil.ErrPersistEOF {
					// TODO: log error
				}
				return
			}
		}
		if err != nil {
			if err != httputil.ErrPersistEOF {
				// TODO: log error
				// TODO: Return 502
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
			if err != httputil.ErrPersistEOF {
				// TODO: log error
			}
			return
		}
		req.Header.Set("X-Request-Start", strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10))
	}
}
