package main

import (
	"sync"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/stream"
)

var testMode = false

type DiscoverdServiceCache interface {
	Addrs() []string
	Close() error
}

func NewDiscoverdServiceCache(s discoverd.Service) (DiscoverdServiceCache, error) {
	d := &discoverdServiceCache{addrs: make(map[string]struct{})}
	return d, d.start(s)
}

type discoverdServiceCache struct {
	stream stream.Stream

	sync.RWMutex
	addrs map[string]struct{}

	// used by the test suite
	watchCh chan *discoverd.Event
}

func (d *discoverdServiceCache) start(s discoverd.Service) (err error) {
	events := make(chan *discoverd.Event)
	d.stream, err = s.Watch(events)
	if err != nil {
		return err
	}
	current := make(chan error)
	go func() {
		for event := range events {
			switch event.Kind {
			case discoverd.EventKindUp, discoverd.EventKindUpdate:
				d.Lock()
				d.addrs[event.Instance.Addr] = struct{}{}
				d.Unlock()
			case discoverd.EventKindDown:
				d.Lock()
				delete(d.addrs, event.Instance.Addr)
				d.Unlock()
			case discoverd.EventKindCurrent:
				if current != nil {
					current <- nil
					current = nil
				}
			}
			if testMode {
				d.Lock()
				if d.watchCh != nil {
					d.watchCh <- event
				}
				d.Unlock()
			}
		}
		if current != nil {
			current <- d.stream.Err()
		}
		// TODO: handle discoverd disconnection
	}()
	return <-current
}

func (d *discoverdServiceCache) Close() error {
	return d.stream.Close()
}

func (d *discoverdServiceCache) Addrs() []string {
	d.RLock()
	defer d.RUnlock()
	res := make([]string, 0, len(d.addrs))
	for addr := range d.addrs {
		res = append(res, addr)
	}
	return res
}

// This method is only used by the test suite
func (d *discoverdServiceCache) watch(current bool) chan *discoverd.Event {
	d.Lock()
	d.watchCh = make(chan *discoverd.Event)
	go func() {
		if current {
			for addr := range d.addrs {
				d.watchCh <- &discoverd.Event{
					Kind:     discoverd.EventKindUp,
					Instance: &discoverd.Instance{Addr: addr},
				}
			}
		}
		d.Unlock()
	}()
	return d.watchCh
}

func (d *discoverdServiceCache) unwatch(ch chan *discoverd.Event) {
	go func() {
		for range ch {
		}
	}()
	d.Lock()
	close(d.watchCh)
	d.watchCh = nil
	d.Unlock()
}
