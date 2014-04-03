package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"strconv"
	"sync"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/strowger/types"
)

func NewTCPListener(ip string, ds DataStore, dc DiscoverdClient) *TCPListener {
	l := &TCPListener{
		IP:        ip,
		ds:        ds,
		wm:        NewWatchManager(),
		discoverd: dc,
		services:  make(map[int]*tcpService),
	}
	l.Watcher = l.wm
	return l
}

type TCPListener struct {
	Watcher

	IP string

	discoverd DiscoverdClient
	ds        DataStore
	wm        *WatchManager

	mtx      sync.RWMutex
	nextPort int
	services map[int]*tcpService
	closed   bool
}

func (l *TCPListener) AddRoute(route *strowger.TCPRoute) error {
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	if l.closed {
		return ErrClosed
	}
	return l.ds.Add(strconv.Itoa(route.Port), route)
}

func (l *TCPListener) RemoveRoute(port string) error {
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	if l.closed {
		return ErrClosed
	}
	return l.ds.Remove(port)
}

func (l *TCPListener) Start() error {
	started := make(chan error)
	go l.ds.Sync(&tcpSyncHandler{l: l}, started)
	return <-started
}

func (l *TCPListener) Close() error {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.ds.StopSync()
	for _, s := range l.services {
		s.Close()
	}
	l.closed = true
	return nil
}

type tcpSyncHandler struct {
	l *TCPListener
}

func (h *tcpSyncHandler) Add(data []byte) error {
	r := &strowger.TCPRoute{}
	if err := json.Unmarshal(data, r); err != nil {
		return err
	}

	h.l.mtx.Lock()
	defer h.l.mtx.Unlock()
	if h.l.closed {
		return nil
	}
	if _, ok := h.l.services[r.Port]; ok {
		return ErrExists
	}

	s := &tcpService{addr: h.l.IP + ":" + strconv.Itoa(r.Port)}
	var err error
	s.ss, err = h.l.discoverd.NewServiceSet(r.Service)
	if err != nil {
		return err
	}

	started := make(chan error)
	go s.Serve(started)
	if err := <-started; err != nil {
		s.ss.Close()
		return err
	}
	h.l.services[r.Port] = s
	go h.l.wm.Send(&strowger.Event{Event: "add", ID: strconv.Itoa(r.Port)})
	return nil
}

func (h *tcpSyncHandler) Remove(id string) error {
	h.l.mtx.Lock()
	defer h.l.mtx.Unlock()
	if h.l.closed {
		return nil
	}

	port, _ := strconv.Atoi(id)
	service, ok := h.l.services[port]
	if !ok {
		return ErrNotFound
	}
	service.Close()
	delete(h.l.services, port)
	go h.l.wm.Send(&strowger.Event{Event: "remove", ID: id})
	return nil
}

type tcpService struct {
	addr string
	l    net.Listener
	ss   discoverd.ServiceSet
}

func (s *tcpService) Close() {
	s.l.Close()
	s.ss.Close()
}

func (s *tcpService) Serve(started chan<- error) {
	var err error
	// TODO: close the listener while there are no backends available
	s.l, err = net.Listen("tcp", s.addr)
	started <- err
	if err != nil {
		return
	}
	for {
		conn, err := s.l.Accept()
		if err != nil {
			break
		}
		go s.handle(conn)
	}
}

func (s *tcpService) getBackend() (conn net.Conn) {
	var err error
	for _, addr := range shuffle(s.ss.Addrs()) {
		// TODO: set deadlines
		conn, err = net.Dial("tcp", addr)
		if err != nil {
			log.Println("Error connecting to TCP backend:", err)
			// TODO: limit number of backends tried
			// TODO: temporarily quarantine failing backends
			continue
		}
		return
	}
	if err == nil {
		log.Println("No TCP backends found")
	} else {
		log.Println("Unable to find live backend, last error:", err)
	}
	return
}

func (s *tcpService) handle(conn net.Conn) {
	defer conn.Close()
	backend := s.getBackend()
	if backend == nil {
		return
	}
	defer backend.Close()

	// TODO: PROXY protocol

	done := make(chan struct{})
	go func() {
		io.Copy(backend, conn)
		backend.(*net.TCPConn).CloseWrite()
		close(done)
	}()
	io.Copy(conn, backend)
	conn.(*net.TCPConn).CloseWrite()
	<-done
	return
}
