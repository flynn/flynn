package dialer

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-discoverd/balancer"
)

// NewHTTPClient returns a HTTP client configured to use discoverd to lookup hostnames.
func NewHTTPClient(c DiscoverdClient) *http.Client {
	return &http.Client{Transport: &http.Transport{Dial: New(c, nil).Dial}}
}

type DialFunc func(network, addr string) (net.Conn, error)

// New returns a Dialer that uses discoverd to lookup hostnames. If f is
// provided, it used to Dial after looking up an address.
func New(c DiscoverdClient, f DialFunc) Dialer {
	return newDialer(c, f)
}

type DiscoverdClient interface {
	NewServiceSet(name string) (discoverd.ServiceSet, error)
}

type Dialer interface {
	Dial(network, addr string) (net.Conn, error)
	Close() error
}

func newDialer(c DiscoverdClient, f DialFunc) *dialer {
	d := &dialer{c: c, sets: make(map[string]*set), dial: f}
	if d.dial == nil {
		d.dial = net.Dial
	}
	return d
}

type dialer struct {
	c    DiscoverdClient
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

func (d *dialer) Close() error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	for k, v := range d.sets {
		v.ServiceSet.Close()
		delete(d.sets, k)
	}
	return nil
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
