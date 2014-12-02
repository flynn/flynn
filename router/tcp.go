package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/kavu/go_reuseport"
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

func (l *TCPListener) Start() error {
	started := make(chan error)

	if l.startPort != 0 && l.endPort != 0 {
		for i := l.startPort; i <= l.endPort; i++ {
			listener, err := reuseport.NewReusablePortListener("tcp4", fmt.Sprintf("%s:%d", l.IP, i))
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
	for _, s := range l.routes {
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
	r := &tcpRoute{
		TCPRoute: route,
		addr:     h.l.IP + ":" + strconv.Itoa(route.Port),
		parent:   h.l,
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
		service = &tcpService{
			name: r.Service,
			ss:   ss,
		}
		h.l.services[r.Service] = service
	}
	r.service = service
	if listener, ok := h.l.listeners[r.Port]; ok {
		r.l = listener
		delete(h.l.listeners, r.Port)
	}
	started := make(chan error)
	go r.Serve(started)
	if err := <-started; err != nil {
		if r.l != nil {
			h.l.listeners[r.Port] = r.l
		}
		return err
	}
	service.refs++
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
	r.Close()

	r.service.refs--
	if r.service.refs <= 0 {
		r.service.ss.Close()
		delete(h.l.services, r.service.name)
	}

	delete(h.l.routes, id)
	delete(h.l.ports, r.Port)
	go h.l.wm.Send(&router.Event{Event: "remove", ID: id})
	return nil
}

type tcpRoute struct {
	parent *TCPListener
	*router.TCPRoute
	l       net.Listener
	addr    string
	service *tcpService
	mtx     sync.RWMutex
}

func (r *tcpRoute) Serve(started chan<- error) {
	var err error
	// TODO: close the listener while there are no backends available
	if r.l == nil {
		r.l, err = reuseport.NewReusablePortListener("tcp4", r.addr)
	}
	started <- err
	if err != nil {
		return
	}
	for {
		conn, err := r.l.Accept()
		if err != nil {
			break
		}
		r.mtx.RLock()
		go r.service.handle(conn)
		r.mtx.RUnlock()
	}
}

func (r *tcpRoute) Close() {
	if r.Port >= r.parent.startPort && r.Port <= r.parent.endPort {
		// make a copy of the fd and create a new listener with it
		fd, err := r.l.(*net.TCPListener).File()
		if err != nil {
			log.Println("Error getting listener fd", r.l)
			return
		}
		r.parent.listeners[r.Port], err = net.FileListener(fd)
		if err != nil {
			log.Println("Error copying listener", r.l)
			return
		}
		fd.Close()
	}
	r.l.Close()
}

type tcpService struct {
	name string
	ss   discoverd.ServiceSet
	refs int
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
