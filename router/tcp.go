package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/router/types"
)

func NewTCPListener(ip string, startPort, endPort int, ds DataStore, dc DiscoverdClient) *TCPListener {
	l := &TCPListener{
		IP:        ip,
		ds:        ds,
		wm:        NewWatchManager(),
		discoverd: dc,
		services:  make(map[string]*tcpService),
		routes:    make(map[string]*tcpRoute),
		ports:     make(map[int]*tcpRoute),
		listeners: make(map[int]net.Listener),
		startPort: startPort,
		endPort:   endPort,
	}
	l.Watcher = l.wm
	l.DataStoreReader = l.ds
	return l
}

type TCPListener struct {
	Watcher
	DataStoreReader

	IP string

	discoverd DiscoverdClient
	ds        DataStore
	wm        *WatchManager

	startPort int
	endPort   int
	listeners map[int]net.Listener

	mtx      sync.RWMutex
	services map[string]*tcpService
	routes   map[string]*tcpRoute
	ports    map[int]*tcpRoute
	closed   bool
}

func (l *TCPListener) AddRoute(route *router.Route) error {
	r := route.TCPRoute()
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	if l.closed {
		return ErrClosed
	}
	if r.Port == 0 {
		return l.addWithAllocatedPort(route)
	}
	route.ID = md5sum(strconv.Itoa(r.Port))
	return l.ds.Add(route)
}

func (l *TCPListener) SetRoute(route *router.Route) error {
	r := route.TCPRoute()
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	if l.closed {
		return ErrClosed
	}
	if r.Port == 0 {
		return errors.New("router: a port number needs to be specified")
	}
	route.ID = md5sum(strconv.Itoa(r.Port))
	return l.ds.Set(route)
}

var ErrNoPorts = errors.New("router: no ports available")

func (l *TCPListener) addWithAllocatedPort(route *router.Route) error {
	r := route.TCPRoute()
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	for r.Port = range l.listeners {
		r.Route.ID = md5sum(strconv.Itoa(r.Port))
		tempRoute := r.ToRoute()
		if err := l.ds.Add(tempRoute); err == nil {
			*route = *tempRoute
			return nil
		}
	}
	return ErrNoPorts
}

func (l *TCPListener) RemoveRoute(id string) error {
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	if l.closed {
		return ErrClosed
	}
	return l.ds.Remove(id)
}

func (s *TCPListener) PauseService(id string, pause bool) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.closed {
		return ErrClosed
	}
	service := s.services[id]
	if pause {
		service.Pause()
	} else {
		service.Unpause()
	}
	return nil
}
func (s *TCPListener) AddDrainListener(serviceID string, ch chan string) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	srv := s.services[serviceID]
	srv.listenerMtx.Lock()
	srv.listeners[ch] = struct{}{}
	srv.listenerMtx.Unlock()
}

func (s *TCPListener) RemoveDrainListener(serviceID string, ch chan string) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	srv := s.services[serviceID]
	srv.listenerMtx.Lock()
	delete(srv.listeners, ch)
	srv.listenerMtx.Unlock()
}

func (l *TCPListener) Start() error {
	started := make(chan error)

	if l.startPort != 0 && l.endPort != 0 {
		for i := l.startPort; i <= l.endPort; i++ {
			listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", l.IP, i))
			if err != nil {
				l.Close()
				return err
			}
			l.listeners[i] = listener
		}
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

func (h *tcpSyncHandler) Set(data *router.Route) error {
	route := data.TCPRoute()
	r := &tcpRoute{TCPRoute: route}

	h.l.mtx.Lock()
	defer h.l.mtx.Unlock()
	if h.l.closed {
		return nil
	}

	service := h.l.services[r.Service]
	if service != nil && service.port != r.Port {
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
		service = &tcpService{
			name:      r.Service,
			addr:      h.l.IP + ":" + strconv.Itoa(r.Port),
			port:      r.Port,
			parent:    h.l,
			ss:        ss,
			paused:    false,
			requests:  make(map[string]int32),
			listeners: make(map[chan string]interface{}),
		}
		service.resumeCond = sync.NewCond(service.pauseMtx.RLocker())
		if listener, ok := h.l.listeners[r.Port]; ok {
			service.l = listener
			delete(h.l.listeners, r.Port)
		}
		started := make(chan error)
		go service.Serve(started)
		if err := <-started; err != nil {
			service.ss.Close()
			if service.l != nil {
				h.l.listeners[r.Port] = service.l
			}
			return err
		}
		h.l.services[r.Service] = service
	}
	service.refs++
	r.service = service
	h.l.routes[data.ID] = r
	h.l.ports[r.Port] = r

	go h.l.wm.Send(&router.Event{Event: "set", ID: data.ID})
	return nil
}

func (h *tcpSyncHandler) Remove(id string) error {
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
		r.service.Close()
		delete(h.l.services, r.service.name)
	}

	delete(h.l.routes, id)
	delete(h.l.ports, r.Port)
	go h.l.wm.Send(&router.Event{Event: "remove", ID: id})
	return nil
}

type tcpRoute struct {
	*router.TCPRoute
	service *tcpService
}

type tcpService struct {
	parent *TCPListener
	addr   string
	port   int
	l      net.Listener
	name   string
	ss     discoverd.ServiceSet
	refs   int

	requests    map[string]int32
	requestMtx  sync.RWMutex
	listeners   map[chan string]interface{}
	listenerMtx sync.RWMutex

	paused     bool
	pauseMtx   sync.RWMutex
	resumeCond *sync.Cond
}

func (s *tcpService) Pause() {
	s.pauseMtx.Lock()
	s.paused = true
	s.pauseMtx.Unlock()
}

func (s *tcpService) Unpause() {
	s.pauseMtx.Lock()
	s.paused = false
	s.pauseMtx.Unlock()
	s.resumeCond.Broadcast()
}

func (s *tcpService) sendEvent(event string) {
	s.listenerMtx.RLock()
	defer s.listenerMtx.RUnlock()
	for ch := range s.listeners {
		ch <- event
	}
}

func (s *tcpService) Close() {
	if s.port >= s.parent.startPort && s.port <= s.parent.endPort {
		// make a copy of the fd and create a new listener with it
		fd, err := s.l.(*net.TCPListener).File()
		if err != nil {
			log.Println("Error getting listener fd", s.l)
			return
		}
		s.parent.listeners[s.port], err = net.FileListener(fd)
		if err != nil {
			log.Println("Error copying listener", s.l)
			return
		}
		fd.Close()
	}
	s.l.Close()
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

func (s *tcpService) getBackend() (net.Conn, string) {
	var err error
	for _, addr := range shuffle(s.ss.Addrs()) {
		// TODO: set deadlines
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			log.Println("Error connecting to TCP backend:", err)
			// TODO: limit number of backends tried
			// TODO: temporarily quarantine failing backends
			continue
		}
		return conn, addr
	}
	if err == nil {
		log.Println("No TCP backends found")
	} else {
		log.Println("Unable to find live backend, last error:", err)
	}
	return nil, ""
}

func (s *tcpService) handle(conn net.Conn) {
	s.pauseMtx.RLock()
	if s.paused {
		s.resumeCond.Wait()
	}
	s.pauseMtx.RUnlock()

	defer conn.Close()
	backend, addr := s.getBackend()
	if backend == nil {
		return
	}
	defer backend.Close()

	s.requestMtx.Lock()
	s.requests[addr]++
	s.requestMtx.Unlock()

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
