package server

import (
	"io"
	"net"

	"github.com/flynn/go-discover/discover"
)

type TCPFrontend struct {
	IP        string
	FirstPort int
	LastPort  int

	allocated map[int]tcpServer
}

type tcpServer struct {
	addr     string
	services *discover.ServiceSet
}

func (b *tcpServer) serve() {
	l, err := net.Listen("tcp", b.addr)
	if err != nil {
		// TODO: log error
		return
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			break
		}
		go b.handle(conn)
	}
}

func (s *tcpServer) getBackend() net.Conn {
	// TODO: randomize backend list
	for _, addr := range s.services.OnlineAddrs() {
		// TODO: set connection timeout
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			// TODO: log error
			// TODO: limit number of backends tried
			// TODO: temporarily quarantine failing backends
			continue
		}
		return conn
	}
	// TODO: log no backends found error
	return nil
}

func (s *tcpServer) handle(conn net.Conn) {
	defer conn.Close()
	backend := s.getBackend()
	if backend == nil {
		return
	}
	// TODO: PROXY protocol
	done := make(chan struct{})
	go func() {
		io.Copy(backend, conn)
		close(done)
	}()
	io.Copy(conn, backend)
	// TODO: handle "temporary" tcp errors?
	<-done
	backend.Close()
	return
}
