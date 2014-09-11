package main

import (
	"errors"
	"flag"
	"net"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	. "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/check.v1"
	"github.com/flynn/flynn/discoverd/agent"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil"
	"github.com/flynn/flynn/discoverd/testutil/etcdrunner"
	"github.com/flynn/flynn/router/types"
)

var fake = flag.Bool("fake", true, "stub out discoverd/etcd")

func init() {
	flag.Parse()
}

type discoverdClient interface {
	DiscoverdClient
	Register(string, string) error
	Unregister(string, string) error
	UnregisterAll() error
	Close() error
}

func newEtcd(t etcdrunner.TestingT) (EtcdClient, string, func()) {
	if *fake {
		return newFakeEtcd(), "", func() {}
	}
	addr, cleanup := etcdrunner.RunEtcdServer(t)
	return etcd.NewClient([]string{addr}), addr, cleanup
}

func newDiscoverd(t etcdrunner.TestingT, etcdPort string) (discoverdClient, func()) {
	if *fake {
		return newFakeDiscoverd(), func() {}
	}
	discoverd, killDiscoverd := testutil.BootDiscoverd(t, "", etcdPort)
	return discoverd, func() {
		discoverd.Close()
		killDiscoverd()
	}
}

func setup(t etcdrunner.TestingT, ec EtcdClient, dc discoverdClient) (discoverdClient, EtcdClient, func()) {
	if *fake {
		if ec == nil {
			ec = newFakeEtcd()
		}
		if dc == nil {
			dc = newFakeDiscoverd()
		}
		return dc, ec, nil
	}
	var killEtcd, killDiscoverd func()
	var etcdAddr string
	if ec == nil {
		etcdAddr, killEtcd = etcdrunner.RunEtcdServer(t)
		ec = etcd.NewClient([]string{etcdAddr})
	} else if c, ok := ec.(*etcd.Client); ok {
		etcdAddr = c.GetCluster()[0]
	}
	if dc == nil {
		dc, killDiscoverd = testutil.BootDiscoverd(t, "", etcdAddr)
	}
	return dc, ec, func() {
		if killDiscoverd != nil {
			dc.Close()
			killDiscoverd()
		}
		if killEtcd != nil {
			killEtcd()
		}
	}
}

func newFakeEtcd() EtcdClient {
	e := &fakeEtcd{
		index:   make(map[string]*etcd.Node),
		root:    &etcd.Node{Key: "/", Dir: true},
		watches: make(map[chan *etcd.Response]watchConfig),
		ch:      make(chan *etcd.Response),
	}
	e.index["/"] = e.root
	go e.watcher()
	return e
}

type fakeEtcd struct {
	mtx   sync.RWMutex
	root  *etcd.Node
	index map[string]*etcd.Node

	ch         chan *etcd.Response
	watchesMtx sync.RWMutex
	watches    map[chan *etcd.Response]watchConfig
}

type watchConfig struct {
	prefix    string
	recursive bool
	stop      chan bool
}

func (e *fakeEtcd) watcher() {
	for r := range e.ch {
		go func(r *etcd.Response) {
			e.watchesMtx.RLock()
			defer e.watchesMtx.RUnlock()
			for ch, conf := range e.watches {
				if r.Node.Key == conf.prefix || conf.recursive && strings.HasPrefix(r.Node.Key, appendSlash(conf.prefix)) {
					select {
					case <-conf.stop:
						go func() {
							e.watchesMtx.Lock()
							delete(e.watches, ch)
							e.watchesMtx.Unlock()
						}()
					case ch <- &etcd.Response{Action: r.Action, Node: deepCopyNode(r.Node)}:
					}
				}
			}
		}(r)
	}
}

func appendSlash(s string) string {
	if !strings.HasSuffix(s, "/") {
		return s + "/"
	}
	return s
}

func deepCopyNode(n *etcd.Node) *etcd.Node {
	newNode := *n
	newNode.Nodes = make(etcd.Nodes, len(n.Nodes))
	for i, n := range n.Nodes {
		newNode.Nodes[i] = deepCopyNode(n)
	}
	return &newNode
}

func (e *fakeEtcd) Watch(prefix string, waitIndex uint64, recursive bool, receiver chan *etcd.Response, stop chan bool) (*etcd.Response, error) {
	if prefix != "/" {
		prefix = strings.TrimSuffix(prefix, "/")
	}
	e.watchesMtx.Lock()
	e.watches[receiver] = watchConfig{prefix: prefix, recursive: recursive, stop: stop}
	e.watchesMtx.Unlock()
	return &etcd.Response{Action: "watch"}, nil
}

func (e *fakeEtcd) Set(key string, value string, ttl uint64) (*etcd.Response, error) {
	return e.set(key, value, ttl, true)
}

func (e *fakeEtcd) Create(key string, value string, ttl uint64) (*etcd.Response, error) {
	return e.set(key, value, ttl, false)
}

func (e *fakeEtcd) set(key string, value string, ttl uint64, allowExist bool) (*etcd.Response, error) {
	if key == "" || key[0] != '/' {
		return nil, errors.New("etcd: key must start with /")
	}
	key = strings.TrimSuffix(key, "/")
	e.mtx.Lock()
	defer e.mtx.Unlock()
	if _, ok := e.index[key]; ok && !allowExist {
		return nil, &etcd.EtcdError{ErrorCode: 105, Message: "Key already exists"}
	}

	components := strings.Split(key, "/")[1:]
	components[0] = "/" + components[0]
	n := e.root
	for i := range components {
		path := strings.Join(components[:i+1], "/")
		if tmp, ok := e.index[path]; ok {
			n = tmp
			continue
		}
		last := i == len(components)-1
		newNode := &etcd.Node{Key: path, Dir: !last}
		if last {
			newNode.Value = value
		}
		n.Nodes = append(n.Nodes, newNode)
		n = newNode
		e.index[path] = n
	}
	e.ch <- &etcd.Response{Action: "create", Node: deepCopyNode(n)}
	return &etcd.Response{Action: "create", Node: deepCopyNode(n)}, nil
}

func (e *fakeEtcd) Get(key string, sort, recursive bool) (*etcd.Response, error) {
	key = strings.TrimSuffix(key, "/")
	e.mtx.RLock()
	defer e.mtx.RUnlock()
	node, ok := e.index[key]
	if !ok {
		return nil, &etcd.EtcdError{ErrorCode: 100, Message: "Key not found"}
	}
	var res *etcd.Node
	if recursive {
		res = deepCopyNode(node)
	} else {
		n := *node
		n.Nodes = nil
		res = &n
	}
	return &etcd.Response{Action: "get", Node: res}, nil
}

func (e *fakeEtcd) Delete(key string, recursive bool) (*etcd.Response, error) {
	key = strings.TrimSuffix(key, "/")
	e.mtx.Lock()
	defer e.mtx.Unlock()

	n, ok := e.index[key]
	if !ok {
		return nil, &etcd.EtcdError{ErrorCode: 100, Message: "Key not found"}
	}
	if !recursive && len(n.Nodes) > 0 {
		return nil, &etcd.EtcdError{ErrorCode: 108, Message: "Directory not empty"}
	}
	delete(e.index, key)
	parent := e.index[path.Dir(key)]
	var idx int
	for i, node := range parent.Nodes {
		if node.Key == key {
			idx = i
			break
		}
	}
	parent.Nodes = append(parent.Nodes[:idx], parent.Nodes[idx+1:]...)

	if recursive {
		key = key + "/"
		for k := range e.index {
			if strings.HasPrefix(k, key) {
				delete(e.index, k)
			}
		}
	}

	e.ch <- &etcd.Response{Action: "delete", Node: deepCopyNode(n)}
	return &etcd.Response{Action: "delete", Node: n}, nil
}

func newFakeDiscoverd() *fakeDiscoverd {
	return &fakeDiscoverd{services: make(map[string]map[string]*discoverd.Service)}
}

type fakeDiscoverd struct {
	mtx      sync.RWMutex
	services map[string]map[string]*discoverd.Service
}

func (d *fakeDiscoverd) Register(name, addr string) error {
	return d.RegisterWithAttributes(name, addr, nil)
}

func (d *fakeDiscoverd) Close() error { return nil }

func (d *fakeDiscoverd) RegisterWithAttributes(name, addr string, attrs map[string]string) error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	if d.services[name] == nil {
		d.services[name] = make(map[string]*discoverd.Service)
	}
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	host, port, _ := net.SplitHostPort(addr)
	d.services[name][addr] = &discoverd.Service{
		Name:  name,
		Host:  host,
		Port:  port,
		Addr:  addr,
		Attrs: attrs,
	}
	return nil
}

func (d *fakeDiscoverd) Unregister(name, addr string) error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	if s, ok := d.services[name]; ok {
		delete(s, addr)
	}
	return nil
}

func (d *fakeDiscoverd) UnregisterAll() error {
	d.mtx.Lock()
	d.services = make(map[string]map[string]*discoverd.Service)
	d.mtx.Unlock()
	return nil
}

func (d *fakeDiscoverd) NewServiceSet(name string) (discoverd.ServiceSet, error) {
	return &fakeServiceSet{d: d, name: name}, nil
}

type fakeServiceSet struct {
	d    *fakeDiscoverd
	name string
}

func (s *fakeServiceSet) SelfAddr() string { return "" }

func (s *fakeServiceSet) Leader() *discoverd.Service { return nil }

func (s *fakeServiceSet) Leaders() chan *discoverd.Service { return nil }

func (s *fakeServiceSet) Services() []*discoverd.Service {
	s.d.mtx.RLock()
	defer s.d.mtx.RUnlock()
	services := s.d.services[s.name]
	res := make([]*discoverd.Service, 0, len(services))
	for _, s := range services {
		res = append(res, s)
	}
	return res
}

func (s *fakeServiceSet) Addrs() []string {
	s.d.mtx.RLock()
	defer s.d.mtx.RUnlock()
	services := s.d.services[s.name]
	res := make([]string, 0, len(services))
	for _, s := range services {
		res = append(res, s.Addr)
	}
	return res
}

func (s *fakeServiceSet) Select(attrs map[string]string) []*discoverd.Service { return nil }

func (s *fakeServiceSet) Filter(attrs map[string]string) {}

func (s *fakeServiceSet) Watch(bringCurrent bool) chan *agent.ServiceUpdate { return nil }

func (s *fakeServiceSet) Unwatch(chan *agent.ServiceUpdate) {}

func (s *fakeServiceSet) Close() error { return nil }

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct{}

var _ = Suite(&S{})

const waitTimeout = time.Second

func waitForEvent(c *C, w Watcher, event string, id string) func() *router.Event {
	ch := make(chan *router.Event)
	w.Watch(ch)
	return func() *router.Event {
		defer w.Unwatch(ch)
		start := time.Now()
		for {
			timeout := waitTimeout - time.Now().Sub(start)
			if timeout <= 0 {
				break
			}
			select {
			case e := <-ch:
				if e.Event == event && (id == "" || e.ID == id) {
					return e
				}
			case <-time.After(timeout):
				break
			}
		}
		c.Fatalf("timeout exceeded waiting for %s %s", event, id)
		return nil
	}
}

func discoverdRegisterTCP(c *C, l *tcpListener, addr string) {
	discoverdRegisterTCPService(c, l, "test", addr)
}

func discoverdRegisterTCPService(c *C, l *tcpListener, name, addr string) {
	dc := l.TCPListener.discoverd.(discoverdClient)
	ss := l.TCPListener.services[name].ss
	discoverdRegister(c, dc, ss, name, addr)
}

func discoverdRegisterHTTP(c *C, l *httpListener, addr string) {
	discoverdRegisterHTTPService(c, l, "test", addr)
}

func discoverdRegisterHTTPService(c *C, l *httpListener, name, addr string) {
	dc := l.HTTPListener.discoverd.(discoverdClient)
	ss := l.HTTPListener.services[name].ss
	discoverdRegister(c, dc, ss, name, addr)
}

func discoverdRegister(c *C, dc discoverdClient, ss discoverd.ServiceSet, name, addr string) {
	done := make(chan struct{})
	if !*fake {
		ch := ss.Watch(false)
		go func() {
			defer ss.Unwatch(ch)
			for u := range ch {
				if u.Addr == addr && u.Online {
					close(done)
					return
				}
			}
		}()
	} else {
		close(done)
	}
	dc.Register(name, addr)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		c.Fatal("timed out waiting for discoverd registration")
	}
}

func discoverdUnregister(c *C, dc discoverdClient, name, addr string) {
	done := make(chan struct{})
	ss, err := dc.NewServiceSet(name)
	c.Assert(err, IsNil)
	if !*fake {
		ch := ss.Watch(false)
		go func() {
			defer ss.Close()
			for u := range ch {
				if u.Addr == addr && !u.Online {
					close(done)
					return
				}
			}
		}()
	} else {
		close(done)
	}
	dc.Unregister(name, addr)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		c.Fatal("timed out waiting for discoverd unregister")
	}
}

func addRoute(c *C, l Listener, r *router.Route) *router.Route {
	wait := waitForEvent(c, l, "set", "")
	err := l.AddRoute(r)
	c.Assert(err, IsNil)
	wait()
	return r
}
