// package balancer provides reference implementations for common load balancing
// techniques across service sets.
package balancer

import (
	"errors"
	"math/rand"
	"sync"

	"github.com/flynn/go-discoverd"
)

var ErrNoServices = errors.New("balancer: no services found")

type LoadBalancer interface {
	Next() (*discoverd.Service, error)
}

type randomBalancer struct {
	set    discoverd.ServiceSet
	random *rand.Rand
	mutex  sync.Mutex
}

func (r *randomBalancer) Next() (*discoverd.Service, error) {
	services := r.set.Services()
	if len(services) == 0 {
		return nil, ErrNoServices
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return services[r.random.Intn(len(services))], nil
}

// Random returns a LoadBalancer that selects random services
// using a rand.Rand with source as the source.
// If source is nil, the default will be used.
func Random(set discoverd.ServiceSet, source rand.Source) LoadBalancer {
	if source == nil {
		source = rand.NewSource(rand.Int63())
	}
	return &randomBalancer{set: set, random: rand.New(source)}
}

type roundRobinBalancer struct {
	set     discoverd.ServiceSet
	current int
	mutex   sync.Mutex
}

func (r *roundRobinBalancer) Next() (*discoverd.Service, error) {
	services := r.set.Services()
	if len(services) == 0 {
		return nil, ErrNoServices
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	res := services[r.current]
	r.current = (r.current + 1) % len(services)
	return res, nil
}

// RoundRobin returns a LoadBalancer that selects services in
// the order they appear in the service set.
func RoundRobin(set discoverd.ServiceSet) LoadBalancer {
	return &roundRobinBalancer{set: set}
}
