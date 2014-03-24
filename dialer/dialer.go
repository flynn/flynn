package dialer

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-discoverd/balancer"
)

// HTTPClient returns a HTTP client configured to use discoverd to lookup hostnames.
func HTTPClient(c *discoverd.Client) *http.Client {
	return &http.Client{Transport: &http.Transport{Dial: Dialer(c, nil)}}
}

type DialFunc func(network, addr string) (net.Conn, error)

// Dialer returns a DialFunc that uses discoverd to lookup hostnames. If f is
// provided, it used to Dial after looking up an address.
func Dialer(c *discoverd.Client, f DialFunc) DialFunc {
	return newDialer(c, f).Dial
}

func newDialer(c *discoverd.Client, f DialFunc) *dialer {
	d := &dialer{c: c, sets: make(map[string]*set), dial: f}
	if d.dial == nil {
		d.dial = net.Dial
	}
	return d
}

type dialer struct {
	c    *discoverd.Client
	dial DialFunc
	sets map[string]*set
	mtx  sync.RWMutex
}

type set struct {
	discoverd.ServiceSet
	balancer.LoadBalancer
}

func (d *dialer) Dial(network, addr string) (net.Conn, error) {
	name := strings.SplitN(addr, ":", 2)[0]
	set, err := d.getSet(name)
	if err != nil {
		return nil, err
	}

	server, err := set.Next()
	if err != nil {
		return nil, err
	}

	return d.dial(network, server.Addr)
}

func (d *dialer) getSet(name string) (*set, error) {
	d.mtx.RLock()
	set := d.sets[name]
	d.mtx.RUnlock()
	if set == nil {
		return d.createSet(name)
	}
	return set, nil
}

func (d *dialer) createSet(name string) (*set, error) {
	d.mtx.Lock()
	defer d.mtx.Unlock()

	if services, ok := d.sets[name]; ok {
		return services, nil
	}

	services, err := d.c.NewServiceSet(name)
	if err != nil {
		return nil, err
	}
	s := &set{services, balancer.Random(services, nil)}
	d.sets[name] = s
	return s, nil
}
