package dialer

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/random"
)

var ErrNoServices = errors.New("dialer: no services found")

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
	d := &dialer{c: c, sets: make(map[string]discoverd.ServiceSet), dial: f}
	if d.dial == nil {
		d.dial = net.Dial
	}
	return d
}

type dialer struct {
	c    DiscoverdClient
	dial DialFunc
	sets map[string]discoverd.ServiceSet
	mtx  sync.RWMutex
}

func (d *dialer) Dial(network, addr string) (conn net.Conn, err error) {
	name := strings.SplitN(addr, ":", 2)[0]
	set, err := d.getSet(name)
	if err != nil {
		return nil, err
	}

	services := set.Services()
	if len(services) == 0 {
		return nil, ErrNoServices
	}

	// try to dial each service in random order until one is found or all have
	// errored
	for _, i := range random.Math.Perm(len(services)) {
		conn, err = d.dial(network, services[i].Addr)
		if err == nil {
			break
		}
	}

	return
}

func (d *dialer) Close() error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	for k, v := range d.sets {
		v.Close()
		delete(d.sets, k)
	}
	return nil
}

func (d *dialer) getSet(name string) (discoverd.ServiceSet, error) {
	d.mtx.RLock()
	set := d.sets[name]
	d.mtx.RUnlock()
	if set == nil {
		return d.createSet(name)
	}
	return set, nil
}

func (d *dialer) createSet(name string) (discoverd.ServiceSet, error) {
	d.mtx.Lock()
	defer d.mtx.Unlock()

	if services, ok := d.sets[name]; ok {
		return services, nil
	}

	services, err := d.c.NewServiceSet(name)
	if err != nil {
		return nil, err
	}
	d.sets[name] = services
	return services, nil
}
