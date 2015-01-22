package main

import (
	"io"
	"testing"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil"
	"github.com/flynn/flynn/discoverd/testutil/etcdrunner"
	"github.com/flynn/flynn/router/types"
)

func init() {
	testMode = true
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

func (d *discoverdWrapper) Close() {
	for _, hb := range d.hbs {
		hb.Close()
	}
}

func newEtcd(t etcdrunner.TestingT) (EtcdClient, string, func()) {
	addr, cleanup := etcdrunner.RunEtcdServer(t)
	return etcd.NewClient([]string{addr}), addr, cleanup
}

func newDiscoverd(t etcdrunner.TestingT, etcdPort string) (discoverdClient, func()) {
	dc, killDiscoverd := testutil.BootDiscoverd(t, "", etcdPort)
	dw := &discoverdWrapper{discoverdClient: dc}
	return dw, func() {
		dw.Close()
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
	dw := &discoverdWrapper{discoverdClient: dc}
	return dw, ec, func() {
		if killDiscoverd != nil {
			dw.Close()
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

func discoverdRegisterTCP(c *C, l *tcpListener, addr string) func() {
	return discoverdRegisterTCPService(c, l, "test", addr)
}

func discoverdRegisterTCPService(c *C, l *tcpListener, name, addr string) func() {
	dc := l.TCPListener.discoverd.(discoverdClient)
	sc := l.TCPListener.services[name].sc
	return discoverdRegister(c, dc, sc.(*discoverdServiceCache), name, addr)
}

func discoverdRegisterHTTP(c *C, l *httpListener, addr string) func() {
	return discoverdRegisterHTTPService(c, l, "test", addr)
}

func discoverdRegisterHTTPService(c *C, l *httpListener, name, addr string) func() {
	dc := l.HTTPListener.discoverd.(discoverdClient)
	sc := l.HTTPListener.services[name].sc
	return discoverdRegister(c, dc, sc.(*discoverdServiceCache), name, addr)
}

func discoverdRegister(c *C, dc discoverdClient, sc *discoverdServiceCache, name, addr string) func() {
	done := make(chan struct{})
	go func() {
		events := sc.watch(true)
		defer sc.unwatch(events)
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

func discoverdUnregisterFunc(c *C, hb discoverd.Heartbeater, sc *discoverdServiceCache) func() {
	return func() {
		done := make(chan struct{})
		started := make(chan struct{})
		go func() {
			events := sc.watch(false)
			defer sc.unwatch(events)
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

func addRoute(c *C, l Listener, r *router.Route) *router.Route {
	wait := waitForEvent(c, l, "set", "")
	err := l.AddRoute(r)
	c.Assert(err, IsNil)
	wait()
	return r
}
