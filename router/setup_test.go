package main

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flynn/flynn/discoverd/cache"
	discoverd "github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/stream"
	router "github.com/flynn/flynn/router/types"
	. "github.com/flynn/go-check"
)

func init() {
	listenFunc = net.Listen
}

type discoverdClient interface {
	DiscoverdClient
	AddServiceAndRegister(string, string) (discoverd.Heartbeater, error)
}

// discoverdWrapper wraps a discoverd client to expose Close method that closes
// all heartbeaters
type discoverdWrapper struct {
	discoverdClient
	hbs []io.Closer
}

func (d *discoverdWrapper) AddServiceAndRegister(service, addr string) (discoverd.Heartbeater, error) {
	hb, err := d.discoverdClient.AddServiceAndRegister(service, addr)
	if err != nil {
		return nil, err
	}
	d.hbs = append(d.hbs, hb)
	return hb, nil
}

func (d *discoverdWrapper) Cleanup() {
	for _, hb := range d.hbs {
		hb.Close()
	}
	d.hbs = nil
}

type testStore struct {
	routesMtx sync.Mutex
	routes    map[string]*router.Route

	streamsMtx sync.Mutex
	streams    map[chan *router.Event]*stream.Basic
}

func newTestStore() *testStore {
	return &testStore{
		routes:  make(map[string]*router.Route),
		streams: make(map[chan *router.Event]*stream.Basic),
	}
}

func (t *testStore) List() ([]*router.Route, error) {
	t.routesMtx.Lock()
	defer t.routesMtx.Unlock()
	routes := make([]*router.Route, 0, len(t.routes))
	for _, r := range t.routes {
		routes = append(routes, r)
	}
	return routes, nil
}

func (t *testStore) Watch(ch chan *router.Event) (stream.Stream, error) {
	s := stream.New()
	t.subscribe(ch, s)
	go func() {
		<-s.StopCh
		t.unsubscribe(ch)
	}()
	return s, nil
}

func (t *testStore) add(r *router.Route) {
	if r.ID == "" {
		r.ID = random.UUID()
	}
	if r.Path == "" {
		r.Path = "/"
	}
	t.routesMtx.Lock()
	t.routes[r.ID] = r
	t.routesMtx.Unlock()
	t.emit(&router.Event{
		Event: router.EventTypeRouteSet,
		ID:    r.ID,
		Route: r,
	})
}

func (t *testStore) update(r *router.Route) {
	t.routesMtx.Lock()
	t.routes[r.ID] = r
	t.routesMtx.Unlock()
	t.emit(&router.Event{
		Event: router.EventTypeRouteSet,
		ID:    r.ID,
		Route: r,
	})
}

func (t *testStore) delete(r *router.Route) {
	t.routesMtx.Lock()
	delete(t.routes, r.ID)
	t.routesMtx.Unlock()
	t.emit(&router.Event{
		Event: router.EventTypeRouteRemove,
		ID:    r.ID,
		Route: r,
	})
}

func (t *testStore) subscribe(ch chan *router.Event, stream *stream.Basic) {
	t.streamsMtx.Lock()
	defer t.streamsMtx.Unlock()
	t.streams[ch] = stream
}

func (t *testStore) unsubscribe(ch chan *router.Event) {
	t.streamsMtx.Lock()
	defer t.streamsMtx.Unlock()
	delete(t.streams, ch)
}

func (t *testStore) emit(event *router.Event) {
	t.streamsMtx.Lock()
	defer t.streamsMtx.Unlock()
	for ch, stream := range t.streams {
		select {
		case ch <- event:
		case <-stream.StopCh:
		}
	}
}

func (t *testStore) closeStreams() {
	t.streamsMtx.Lock()
	defer t.streamsMtx.Unlock()
	for ch, stream := range t.streams {
		stream.Error = errors.New("sync stopped")
		close(ch)
	}
	t.streams = make(map[chan *router.Event]*stream.Basic)
}

func (t *testStore) cleanup() {
	t.routesMtx.Lock()
	defer t.routesMtx.Unlock()
	t.routes = make(map[string]*router.Route)
}

func setup(t testutil.TestingT) (*discoverdWrapper, func()) {
	dc, killDiscoverd := testutil.BootDiscoverd(t, "")
	dw := &discoverdWrapper{discoverdClient: dc}

	return dw, func() {
		killDiscoverd()
	}
}

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
	discoverd *discoverdWrapper
	store     *testStore
	cleanup   func()
}

var _ = Suite(&S{})

func (s *S) SetUpSuite(c *C) {
	s.discoverd, s.cleanup = setup(c)
	s.store = newTestStore()
}

func (s *S) TearDownSuite(c *C) {
	s.cleanup()
}

func (s *S) TearDownTest(c *C) {
	s.discoverd.Cleanup()
	s.store.cleanup()
}

const waitTimeout = time.Second

func waitForEvent(c *C, w Watcher, event router.EventType, id string) func() *router.Event {
	ch := make(chan *router.Event)
	w.Watch(ch, false)
	return func() *router.Event {
		defer w.Unwatch(ch)
		for {
			timeout := time.After(waitTimeout)
			select {
			case e := <-ch:
				if e.Event == event && (id == "" || e.ID == id) {
					return e
				}
			case <-timeout:
				c.Fatalf("timeout exceeded waiting for %s %s", event, id)
				return nil
			}
		}
	}
}

func discoverdRegisterTCP(c *C, l *TCPListener, addr string) func() {
	return discoverdRegisterTCPService(c, l, "test", addr)
}

func discoverdRegisterTCPService(c *C, l *TCPListener, name, addr string) func() {
	dc := l.discoverd.(discoverdClient)
	sc := l.services[name].sc
	return discoverdRegister(c, dc, sc, name, addr)
}

func discoverdRegisterHTTP(c *C, l *HTTPListener, addr string) func() {
	return discoverdRegisterHTTPService(c, l, "test", addr)
}

func discoverdRegisterHTTPService(c *C, l *HTTPListener, name, addr string) func() {
	dc := l.discoverd.(discoverdClient)
	sc := l.services[name].sc
	return discoverdRegister(c, dc, sc, name, addr)
}

func discoverdSetLeaderHTTP(c *C, l *HTTPListener, name, id string) {
	dc := l.discoverd.(discoverdClient)
	sc := l.services[name].sc
	discoverdSetLeader(c, dc, sc, name, id)
}

func discoverdSetLeaderTCP(c *C, l *TCPListener, name, id string) {
	dc := l.discoverd.(discoverdClient)
	sc := l.services[name].sc
	discoverdSetLeader(c, dc, sc, name, id)
}

func discoverdSetLeader(c *C, dc discoverdClient, sc *cache.ServiceCache, name, id string) {
	done := make(chan struct{})
	go func() {
		events := make(chan *discoverd.Event)
		stream := sc.Watch(events, true)
		defer stream.Close()
		for event := range events {
			if event.Kind == discoverd.EventKindLeader && event.Instance.ID == id {
				close(done)
				return
			}
		}
	}()
	err := dc.Service(name).SetLeader(id)
	c.Assert(err, IsNil)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		c.Fatal("timed out waiting for discoverd leader change")
	}
}

func discoverdRegister(c *C, dc discoverdClient, sc *cache.ServiceCache, name, addr string) func() {
	done := make(chan struct{})
	go func() {
		events := make(chan *discoverd.Event)
		stream := sc.Watch(events, true)
		defer stream.Close()
		for event := range events {
			if event.Kind == discoverd.EventKindUp && event.Instance.Addr == addr {
				close(done)
				return
			}
		}
	}()
	hb, err := dc.AddServiceAndRegister(name, addr)
	c.Assert(err, IsNil)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		c.Fatal("timed out waiting for discoverd registration")
	}
	return discoverdUnregisterFunc(c, hb, sc)
}

func discoverdUnregisterFunc(c *C, hb discoverd.Heartbeater, sc *cache.ServiceCache) func() {
	return func() {
		done := make(chan struct{})
		started := make(chan struct{})
		go func() {
			events := make(chan *discoverd.Event)
			stream := sc.Watch(events, false)
			defer stream.Close()
			close(started)
			for event := range events {
				if event.Kind == discoverd.EventKindDown && event.Instance.Addr == hb.Addr() {
					close(done)
					return
				}
			}
		}()
		<-started
		c.Assert(hb.Close(), IsNil)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			c.Fatal("timed out waiting for discoverd unregister")
		}
	}
}

func (s *S) addRoute(c *C, l Listener, r *router.Route) *router.Route {
	return addRoute(c, l, s.store, r)
}

func addRoute(c *C, l Listener, store *testStore, r *router.Route) *router.Route {
	wait := waitForEvent(c, l, "set", "")
	store.add(r)
	wait()
	return r
}

func (s *S) removeRoute(c *C, l Listener, r *router.Route) {
	wait := waitForEvent(c, l, "remove", "")
	s.store.delete(r)
	wait()
}

var portAlloc uint32 = 4500

func allocatePort() int {
	return int(atomic.AddUint32(&portAlloc, 1))
}

func allocatePortRange(count int) (int, int) {
	max := int(atomic.AddUint32(&portAlloc, uint32(count)))
	return max - (count - 1), max
}
