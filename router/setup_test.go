package main

import (
	"testing"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil"
	"github.com/flynn/flynn/discoverd/testutil/etcdrunner"
	"github.com/flynn/flynn/router/types"
)

type discoverdClient interface {
	DiscoverdClient
	Register(string, string) error
	Unregister(string, string) error
	UnregisterAll() error
	Close() error
}

func newEtcd(t etcdrunner.TestingT) (EtcdClient, string, func()) {
	addr, cleanup := etcdrunner.RunEtcdServer(t)
	return etcd.NewClient([]string{addr}), addr, cleanup
}

func newDiscoverd(t etcdrunner.TestingT, etcdPort string) (discoverdClient, func()) {
	discoverd, killDiscoverd := testutil.BootDiscoverd(t, "", etcdPort)
	return discoverd, func() {
		discoverd.Close()
		killDiscoverd()
	}
}

func setup(t etcdrunner.TestingT, ec EtcdClient, dc discoverdClient) (discoverdClient, EtcdClient, func()) {
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
