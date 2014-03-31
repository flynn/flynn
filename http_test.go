package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/flynn/discoverd/agent"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-etcd/etcd"
	. "github.com/titanous/gocheck"
)

func newFakeEtcd() *fakeEtcd {
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
		go func() {
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
		}()
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
	fmt.Printf("%#v\n", e.watches[receiver])
	e.watchesMtx.Unlock()
	return &etcd.Response{Action: "watch"}, nil
}

func (e *fakeEtcd) Create(key string, value string, ttl uint64) (*etcd.Response, error) {
	if key == "" || key[0] != '/' {
		return nil, errors.New("etcd: key must start with /")
	}
	key = strings.TrimSuffix(key, "/")
	e.mtx.Lock()
	defer e.mtx.Unlock()
	if _, ok := e.index[key]; ok {
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

type S struct {
	etcd      *fakeEtcd
	discoverd *fakeDiscoverd
}

var _ = Suite(&S{})

func (s *S) SetUpSuite(c *C) {
	s.etcd = newFakeEtcd()
	s.discoverd = newFakeDiscoverd()
}

func httpTestHandler(id string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(id))
	})
}

func (s *S) newHTTPFrontend() (*HTTPFrontend, error) {
	fe := NewHTTPFrontend("127.0.0.1:0", "127.0.0.1:0", s.etcd, s.discoverd)
	return fe, fe.Start()
}

func (s *S) TestAddHTTPDomain(c *C) {
	srv1 := httptest.NewServer(httpTestHandler("1"))
	srv2 := httptest.NewServer(httpTestHandler("2"))
	defer srv1.Close()
	defer srv2.Close()

	s.discoverd.Register("test", srv1.Listener.Addr().String())
	defer s.discoverd.UnregisterAll()

	fe, err := s.newHTTPFrontend()
	c.Assert(err, IsNil)
	defer fe.Close()

	err = fe.AddHTTPDomain("example.com", "test", nil, nil)
	c.Assert(err, IsNil)

	req, err := http.NewRequest("GET", "http://"+fe.Addr, nil)
	c.Assert(err, IsNil)
	req.Host = "example.com"
	res, err := http.DefaultClient.Do(req)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	data, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "1")

	s.discoverd.Unregister("test", srv1.Listener.Addr().String())
	s.discoverd.Register("test", srv2.Listener.Addr().String())

	// Close the connection we just used to trigger a new backend choice
	http.DefaultTransport.(*http.Transport).CloseIdleConnections()

	res, err = http.DefaultClient.Do(req)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	data, err = ioutil.ReadAll(res.Body)
	res.Body.Close()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "2")

	err = fe.RemoveHTTPDomain("example.com")
	c.Assert(err, IsNil)
	http.DefaultTransport.(*http.Transport).CloseIdleConnections()

	res, err = http.DefaultClient.Do(req)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 404)
	res.Body.Close()
}
