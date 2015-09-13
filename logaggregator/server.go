package main

import (
	"bufio"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/logaggregator/snapshot"
	"github.com/flynn/flynn/logaggregator/utils"
	"github.com/flynn/flynn/pkg/syslog/rfc6587"
)

type Server struct {
	*Aggregator
	Cursors *HostCursors

	ll, al net.Listener   // syslog and api listeners
	lwg    sync.WaitGroup // syslog wait group

	hb discoverd.Heartbeater

	api      http.Handler
	shutdown chan struct{}
}

type ServerConfig struct {
	SyslogAddr, ApiAddr string

	ServiceName string
	Discoverd   *discoverd.Client
}

func NewServer(conf ServerConfig) (*Server, error) {
	ll, err := net.Listen("tcp", conf.SyslogAddr)
	if err != nil {
		return nil, err
	}

	al, err := net.Listen("tcp", conf.ApiAddr)
	if err != nil {
		return nil, err
	}

	var hb discoverd.Heartbeater
	if conf.Discoverd != nil {
		hb, err = conf.Discoverd.AddServiceAndRegister(conf.ServiceName, ll.Addr().String())
		if err != nil {
			return nil, err
		}
	}

	a := NewAggregator()
	c := NewHostCursors()

	return &Server{
		Aggregator: a,
		Cursors:    c,

		ll: ll,
		al: al,
		hb: hb,

		api:      apiHandler(a, c),
		shutdown: make(chan struct{}),
	}, nil
}

func (s *Server) Shutdown() {
	if s.hb != nil {
		// close discoverd service heartbeater
		if err := s.hb.Close(); err != nil {
			log15.Error("heartbeat shutdown error", "err", err)
		}
	}

	// shutdown listeners
	if err := s.ll.Close(); err != nil {
		log15.Error("syslog listener shutdown error", "err", err)
	}
	if err := s.al.Close(); err != nil {
		log15.Error("api listener shutdown error", "err", err)
	}

	// close syslog client connections
	close(s.shutdown)
	s.lwg.Wait()

	// shutdown aggregator
	s.Aggregator.Shutdown()
}

func (s *Server) LoadSnapshot(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	sc := snapshot.NewScanner(f)
	for sc.Scan() {
		cursor, err := utils.ParseHostCursor(sc.Message)
		if err != nil {
			return err
		}
		s.Cursors.Update(string(sc.Message.Hostname), cursor)
		s.Aggregator.Feed(sc.Message)
	}
	return sc.Err()
}

func (s *Server) WriteSnapshot(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	buffers := s.Aggregator.ReadAll()
	return snapshot.WriteTo(buffers, f)
}

func (s *Server) SyslogAddr() net.Addr {
	return s.ll.Addr()
}

func (s *Server) Run() error {
	go s.runSyslog()

	return http.Serve(s.al, s.api)
}

func (s *Server) runSyslog() {
	for {
		conn, err := s.ll.Accept()
		if err != nil {
			return
		}

		s.lwg.Add(1)
		go func(c net.Conn) {
			defer s.lwg.Done()
			s.drainSyslogConn(c)
		}(conn)
	}
}

func (s *Server) drainSyslogConn(conn net.Conn) {
	connDone := make(chan struct{})
	defer close(connDone)

	go func() {
		select {
		case <-connDone:
		case <-s.shutdown:
		}
		conn.Close()
	}()

	sc := bufio.NewScanner(conn)
	sc.Split(rfc6587.Split)
	for sc.Scan() {
		msgBytes := sc.Bytes()
		// slice in msgBytes could get modified on next Scan(), need to copy it
		msgCopy := make([]byte, len(msgBytes))
		copy(msgCopy, msgBytes)

		msg, cursor, err := utils.ParseMessage(msgCopy)
		if err != nil {
			log15.Error("rfc5424 parse error", "err", err)
		} else {
			s.Cursors.Update(string(msg.Hostname), cursor)
			s.Aggregator.Feed(msg)
		}
	}
}

func NewHostCursors() *HostCursors {
	return &HostCursors{Data: make(map[string]*utils.HostCursor)}
}

// HostCursors are used to keep track of what messages a host has sent when
// resuming log streaming to a new server or after an interruption .
type HostCursors struct {
	mtx  sync.Mutex
	Data map[string]*utils.HostCursor
}

func (h *HostCursors) Update(id string, other *utils.HostCursor) {
	h.mtx.Lock()
	defer h.mtx.Unlock()
	curr, ok := h.Data[id]
	if !ok || other.After(*curr) {
		h.Data[id] = other
	}
}

func (h *HostCursors) Get() map[string]*utils.HostCursor {
	h.mtx.Lock()
	defer h.mtx.Unlock()
	res := make(map[string]*utils.HostCursor, len(h.Data))
	for k, v := range h.Data {
		res[k] = v
	}
	return res
}
