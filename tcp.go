package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/strowger/types"
)

func NewTCPListener(ip string, startPort, endPort int, ds DataStore, dc DiscoverdClient) *TCPListener {
	l := &TCPListener{
		IP:        ip,
		ds:        ds,
		wm:        NewWatchManager(),
		discoverd: dc,
		services:  make(map[int]*tcpService),
		listeners: make(map[int]net.Listener),
		startPort: startPort,
		endPort:   endPort,
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

	startPort int
	endPort   int
	listeners map[int]net.Listener

	mtx      sync.RWMutex
	services map[int]*tcpService
	closed   bool
}

func (l *TCPListener) AddRoute(r *strowger.TCPRoute) error {
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	if l.closed {
		return ErrClosed
	}
	if r.Port == 0 {
		return l.addWithAllocatedPort(r)
	}
	return l.ds.Add(strconv.Itoa(r.Port), r)
}

var ErrNoPorts = errors.New("strowger: no ports available")

func (l *TCPListener) addWithAllocatedPort(r *strowger.TCPRoute) error {
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	for r.Port = range l.listeners {
		if err := l.ds.Add(strconv.Itoa(r.Port), r); err == nil {
			return nil
		}
	}
	return ErrNoPorts
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

	for i := l.startPort; i <= l.endPort; i++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", l.IP, i))
		if err != nil {
			l.Close()
			return err
		}
		l.listeners[i] = listener
	}

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
	for _, listener := range l.listeners {
		listener.Close()
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

	s := &tcpService{
		addr:   h.l.IP + ":" + strconv.Itoa(r.Port),
		port:   r.Port,
		parent: h.l,
	}
	var err error
	s.ss, err = h.l.discoverd.NewServiceSet(r.Service)
	if err != nil {
		return err
	}

	if listener, ok := h.l.listeners[r.Port]; ok {
		s.l = listener
		delete(h.l.listeners, r.Port)
	}

	started := make(chan error)
	go s.Serve(started)
	if err := <-started; err != nil {
		s.ss.Close()
		if s.l != nil {
			h.l.listeners[r.Port] = s.l
		}
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
	parent *TCPListener
	addr   string
	port   int
	l      net.Listener
	ss     discoverd.ServiceSet
}

func (s *tcpService) Close() {
	if s.port >= s.parent.startPort && s.port <= s.parent.endPort {
		s.parent.listeners[s.port] = s.l
	} else {
		s.l.Close()
	}
	s.ss.Close()
}

func (s *tcpService) Serve(started chan<- error) {
	var err error
	// TODO: close the listener while there are no backends available
	if s.l == nil {
		s.l, err = net.Listen("tcp", s.addr)
	}
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
