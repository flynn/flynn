package ports

import (
	"errors"
	"sync"
)

func NewAllocator(start, end uint16) *Allocator {
	return &Allocator{start: start, end: end, ports: make(map[uint16]struct{})}
}

type Allocator struct {
	start, end uint16
	ports      map[uint16]struct{}
	mtx        sync.Mutex
}

var ErrInUse = errors.New("ports: port is already in use")
var ErrNoPorts = errors.New("ports: all ports are allocated")

func (a *Allocator) Get() (uint16, error) {
	port := a.start
	for {
		if _, allocated := a.ports[port]; !allocated {
			break
		}
		port++
		if port > a.end {
			return 0, ErrNoPorts
		}
	}
	a.ports[port] = struct{}{}
	return port, nil
}

func (a *Allocator) GetPort(port uint16) (uint16, error) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	if _, allocated := a.ports[port]; allocated {
		return 0, ErrInUse
	}
	a.ports[port] = struct{}{}
	return port, nil
}

func (a *Allocator) Put(port uint16) {
	a.mtx.Lock()
	delete(a.ports, port)
	a.mtx.Unlock()
}
