package main

import (
	"sync"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/stream"
)

var testMode = false

type DiscoverdServiceCache interface {
	Addrs() []string
	Close() error
}

func NewDiscoverdServiceCache(s discoverd.Service) (DiscoverdServiceCache, error) {
	d := &discoverdServiceCache{
		addrs: make(map[string]struct{}),
		stop:  make(chan struct{}),
	}
	return d, d.start(s)
}

type discoverdServiceCache struct {
	stream stream.Stream

	sync.RWMutex
	addrs map[string]struct{}

	// used by the test suite
	watchCh chan *discoverd.Event

	stop chan struct{}
}

var connectAttempts = attempt.Strategy{
	Total: 10 * time.Minute,
	Delay: 500 * time.Millisecond,
}

func (d *discoverdServiceCache) start(s discoverd.Service) (err error) {
	// use a function to create the watcher so we can reconnect if it closes
	// unexpectedly (ideally the discoverd client would use a ResumingStream
	// but service events do not yet support it).
	var events chan *discoverd.Event
	connect := func() (err error) {
		events = make(chan *discoverd.Event)
		d.stream, err = s.Watch(events)
		return
	}
	if err := connect(); err != nil {
		return err
	}
	var once sync.Once
	current := make(chan error)
	go func() {
		for {
			select {
			case <-d.stop:
				return
			case event, ok := <-events:
				if !ok {
					if err := connectAttempts.Run(connect); err != nil {
						once.Do(func() { current <- err })
						return
					}
					continue
				}

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
					once.Do(func() { current <- nil })
				}
				if testMode {
					d.Lock()
					if d.watchCh != nil {
						d.watchCh <- event
					}
					d.Unlock()
				}
			}
		}
	}()
	return <-current
}

func (d *discoverdServiceCache) Close() error {
	close(d.stop)
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
