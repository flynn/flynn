package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/logaggregator/snapshot"
	"github.com/flynn/flynn/logaggregator/utils"
	"github.com/flynn/flynn/pkg/keepalive"
	"github.com/flynn/flynn/pkg/syslog/rfc6587"
	"github.com/inconshreveable/log15"
)

type Server struct {
	*Aggregator
	Cursors *HostCursors

	conf ServerConfig

	syslogListener net.Listener
	apiListener    net.Listener
	syslogWg       sync.WaitGroup
	syslogDone     chan struct{}

	hb discoverd.Heartbeater

	api      http.Handler
	shutdown chan struct{}

	testMessageHook chan struct{}
}

type ServerConfig struct {
	SyslogAddr, ApiAddr string

	ServiceName string
	Discoverd   *discoverd.Client
}

func NewServer(conf ServerConfig) *Server {
	a := NewAggregator()
	c := NewHostCursors()
	return &Server{
		Aggregator: a,
		Cursors:    c,
		conf:       conf,
		api:        apiHandler(a, c),
		shutdown:   make(chan struct{}),
		syslogDone: make(chan struct{}),
	}
}

func (s *Server) Shutdown() {
	if s.hb != nil {
		// close discoverd service heartbeater
		if err := s.hb.Close(); err != nil {
			log15.Error("heartbeat shutdown error", "err", err)
		}
	}

	// shutdown listeners
	if s.syslogListener != nil {
		if err := s.syslogListener.Close(); err != nil {
			log15.Error("syslog listener shutdown error", "err", err)
		}
		<-s.syslogDone
	}
	if s.apiListener != nil {
		if err := s.apiListener.Close(); err != nil {
			log15.Error("api listener shutdown error", "err", err)
		}
	}

	// close syslog client connections
	close(s.shutdown)
	s.syslogWg.Wait()

	// shutdown aggregator
	s.Aggregator.Shutdown()
}

func (s *Server) LoadSnapshotFile(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	return s.LoadSnapshot(f)
}

func (s *Server) LoadSnapshot(r io.Reader) error {
	sc := snapshot.NewScanner(r)
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

func (s *Server) WriteSnapshotFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return snapshot.WriteTo(s.Aggregator.ReadAll(), f)
}

func (s *Server) SyslogAddr() net.Addr {
	return s.syslogListener.Addr()
}

func (s *Server) Start() error {
	var err error
	sl, err := net.Listen("tcp", s.conf.SyslogAddr)
	if err != nil {
		return err
	}
	s.syslogListener = keepalive.Listener(sl)

	al, err := net.Listen("tcp", s.conf.ApiAddr)
	if err != nil {
		return err
	}
	s.apiListener = keepalive.Listener(al)

	if s.conf.Discoverd != nil {
		s.hb, err = s.conf.Discoverd.AddServiceAndRegister(s.conf.ServiceName, s.conf.SyslogAddr)
		if err != nil {
			return err
		}
	}

	go s.runSyslog()
	go http.Serve(s.apiListener, s.api)

	return nil
}

func (s *Server) runSyslog() {
	defer close(s.syslogDone)
	for {
		conn, err := s.syslogListener.Accept()
		if err != nil {
			return
		}

		s.syslogWg.Add(1)
		go func(c net.Conn) {
			defer s.syslogWg.Done()
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
		if s.testMessageHook != nil {
			s.testMessageHook <- struct{}{}
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
