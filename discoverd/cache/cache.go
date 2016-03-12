package cache

import (
	"sync"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/stream"
)

var TestMode = false

type ServiceCache interface {
	LeaderAddr() []string
	Addrs() []string
	Close() error
}

func New(s discoverd.Service) (ServiceCache, error) {
	d := &serviceCache{
		addrs: make(map[string]struct{}),
		stop:  make(chan struct{}),
	}
	return d, d.start(s)
}

type serviceCache struct {
	stream stream.Stream

	sync.RWMutex
	leaderAddr string
	addrs      map[string]struct{}

	// used by the test suite
	watchers map[chan *discoverd.Event]struct{}

	stop chan struct{}
}

var connectAttempts = attempt.Strategy{
	Total: 10 * time.Minute,
	Delay: 500 * time.Millisecond,
}

func (d *serviceCache) start(s discoverd.Service) (err error) {
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
				case discoverd.EventKindLeader:
					d.Lock()
					if event.Instance != nil {
						d.leaderAddr = event.Instance.Addr
					} else {
						d.leaderAddr = ""
					}
					d.Unlock()
				case discoverd.EventKindCurrent:
					once.Do(func() { current <- nil })
				}
				d.broadcast(event)
			}
		}
	}()
	return <-current
}

func (d *serviceCache) Close() error {
	close(d.stop)
	return d.stream.Close()
}

func (d *serviceCache) Addrs() []string {
	d.RLock()
	defer d.RUnlock()
	res := make([]string, 0, len(d.addrs))
	for addr := range d.addrs {
		res = append(res, addr)
	}
	return res
}

func (d *serviceCache) LeaderAddr() []string {
	d.RLock()
	defer d.RUnlock()
	if d.leaderAddr == "" {
		return []string{}
	}
	return []string{d.leaderAddr}
}

func (d *serviceCache) broadcast(e *discoverd.Event) {
	if !TestMode {
		return
	}
	d.RLock()
	defer d.RUnlock()
	for watcher := range d.watchers {
		watcher <- e
	}
}

// This method is only used by the test suite
func (d *serviceCache) Watch(current bool) (chan *discoverd.Event, func()) {
	d.Lock()
	if d.watchers == nil {
		d.watchers = make(map[chan *discoverd.Event]struct{})
	}
	ch := make(chan *discoverd.Event)
	d.watchers[ch] = struct{}{}
	go func() {
		if current {
			for addr := range d.addrs {
				ch <- &discoverd.Event{
					Kind:     discoverd.EventKindUp,
					Instance: &discoverd.Instance{Addr: addr},
				}
			}
		}
		d.Unlock()
	}()
	return ch, func() {
		go func() {
			for range ch {
			}
		}()
		d.Lock()
		defer d.Unlock()
		delete(d.watchers, ch)
		close(ch)
	}
}
