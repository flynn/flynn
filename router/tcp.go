package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/discoverd/cache"
	"github.com/flynn/flynn/pkg/connutil"
	"github.com/flynn/flynn/router/proxy"
	"github.com/flynn/flynn/router/types"
)

type TCPListener struct {
	Watcher
	DataStoreReader

	IP string

	discoverd DiscoverdClient
	ds        DataStore
	wm        *WatchManager
	stopSync  func()

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

func (l *TCPListener) UpdateRoute(route *router.Route) error {
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
	return l.ds.Update(route)
}

var ErrNoPorts = errors.New("router: no ports available")

func (l *TCPListener) addWithAllocatedPort(route *router.Route) error {
	r := route.TCPRoute()
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	for r.Port = range l.listeners {
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
	ctx := context.Background() // TODO(benburkert): make this an argument
	ctx, l.stopSync = context.WithCancel(ctx)

	if l.Watcher != nil {
		return errors.New("router: tcp listener already started")
	}
	if l.wm == nil {
		l.wm = NewWatchManager()
	}
	l.Watcher = l.wm

	if l.ds == nil {
		return errors.New("router: tcp listener missing data store")
	}
	l.DataStoreReader = l.ds

	l.services = make(map[string]*tcpService)
	l.routes = make(map[string]*tcpRoute)
	l.ports = make(map[int]*tcpRoute)
	l.listeners = make(map[int]net.Listener)

	if l.startPort != 0 && l.endPort != 0 {
		for i := l.startPort; i <= l.endPort; i++ {
			addr := fmt.Sprintf("%s:%d", l.IP, i)
			listener, err := listenFunc("tcp4", addr)
			if err != nil {
				l.Close()
				return listenErr{addr, err}
			}
			l.listeners[i] = listener
		}
	}

	// TODO(benburkert): the sync API cannot handle routes deleted while the
	// listen/notify connection is disconnected
	if err := l.startSync(ctx); err != nil {
		return err
	}

	return nil
}

func (l *TCPListener) startSync(ctx context.Context) error {
	errc := make(chan error)
	startc := l.doSync(ctx, errc)

	select {
	case err := <-errc:
		return err
	case <-startc:
		go l.runSync(ctx, errc)
		return nil
	}
}

func (l *TCPListener) runSync(ctx context.Context, errc chan error) {
	err := <-errc

	for {
		if err == nil {
			return
		}
		log.Printf("router: tcp sync error: %s", err)

		time.Sleep(2 * time.Second)

		l.doSync(ctx, errc)

		err = <-errc
	}
}

func (l *TCPListener) doSync(ctx context.Context, errc chan<- error) <-chan struct{} {
	startc := make(chan struct{})

	go func() { errc <- l.ds.Sync(ctx, &tcpSyncHandler{l: l}, startc) }()

	return startc
}

func (l *TCPListener) Close() error {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.stopSync()
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
			service.sc.Close()
			delete(h.l.services, service.name)
		}
		service = nil
	}
	if service == nil {
		sc, err := cache.New(h.l.discoverd.Service(r.Service))
		if err != nil {
			return err
		}
		service = &tcpService{
			name: r.Service,
			sc:   sc,
			rp:   proxy.NewReverseProxy(sc.Addrs, nil, false),
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

	go h.l.wm.Send(&router.Event{Event: "set", ID: data.ID, Route: r.ToRoute()})
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
		r.service.sc.Close()
		delete(h.l.services, r.service.name)
	}

	delete(h.l.routes, id)
	delete(h.l.ports, r.Port)
	go h.l.wm.Send(&router.Event{Event: "remove", ID: id, Route: r.ToRoute()})
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
		r.l, err = listenFunc("tcp4", r.addr)
	}
	if err != nil {
		err = listenErr{r.addr, err}
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
		go r.service.ServeConn(conn)
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
	sc   cache.ServiceCache
	refs int

	rp *proxy.ReverseProxy
}

func (s *tcpService) ServeConn(conn net.Conn) {
	s.rp.ServeConn(context.Background(), connutil.CloseNotifyConn(conn))
}
