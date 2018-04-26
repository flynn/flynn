package cache

import (
	"sync"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/stream"
)

func New(s discoverd.Service) (*ServiceCache, error) {
	d := &ServiceCache{
		instances: make(map[string]*discoverd.Instance),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	return d, d.start(s)
}

type ServiceCache struct {
	stream stream.Stream

	sync.RWMutex
	leader    *discoverd.Instance
	instances map[string]*discoverd.Instance

	// used by the test suite
	watchers map[chan *discoverd.Event]struct{}

	stop chan struct{}
	done chan struct{}
}

var connectAttempts = attempt.Strategy{
	Total: 10 * time.Minute,
	Delay: 500 * time.Millisecond,
}

func (d *ServiceCache) start(s discoverd.Service) (err error) {
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
		defer close(d.done)
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
					d.instances[event.Instance.ID] = event.Instance
					d.Unlock()
				case discoverd.EventKindDown:
					d.Lock()
					delete(d.instances, event.Instance.ID)
					d.Unlock()
				case discoverd.EventKindLeader:
					d.Lock()
					if event.Instance != nil {
						d.leader = event.Instance
					} else {
						d.leader = nil
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

func (d *ServiceCache) Close() error {
	close(d.stop)
	return d.stream.Close()
}

func (d *ServiceCache) Addrs() []string {
	d.RLock()
	defer d.RUnlock()
	res := make([]string, 0, len(d.instances))
	for _, inst := range d.instances {
		res = append(res, inst.Addr)
	}
	return res
}

func (d *ServiceCache) Instances() []*discoverd.Instance {
	d.RLock()
	defer d.RUnlock()
	res := make([]*discoverd.Instance, 0, len(d.instances))
	for _, inst := range d.instances {
		res = append(res, inst)
	}
	return res
}

func (d *ServiceCache) LeaderAddr() []string {
	d.RLock()
	defer d.RUnlock()
	if d.leader == nil {
		return []string{}
	}
	return []string{d.leader.Addr}
}

func (d *ServiceCache) Leader() []*discoverd.Instance {
	d.RLock()
	defer d.RUnlock()
	if d.leader == nil {
		return []*discoverd.Instance{}
	}
	return []*discoverd.Instance{d.leader}
}

func (d *ServiceCache) broadcast(e *discoverd.Event) {
	d.RLock()
	defer d.RUnlock()
	for watcher := range d.watchers {
		watcher <- e
	}
}

func (d *ServiceCache) Watch(ch chan *discoverd.Event, current bool) stream.Stream {
	d.Lock()
	if d.watchers == nil {
		d.watchers = make(map[chan *discoverd.Event]struct{})
	}
	d.watchers[ch] = struct{}{}
	stream := stream.New()
	go func() {
		defer func() {
			d.Lock()
			defer d.Unlock()
			delete(d.watchers, ch)
		}()

		if current {
			for _, inst := range d.instances {
				select {
				case ch <- &discoverd.Event{
					Kind:     discoverd.EventKindUp,
					Instance: inst,
				}:
				case <-stream.StopCh:
					go func() {
						for range ch {
						}
					}()
					d.Unlock()
					return
				case <-d.done:
					close(ch)
					d.Unlock()
					return
				}
			}
		}
		d.Unlock()
		select {
		case <-stream.StopCh:
			go func() {
				for range ch {
				}
			}()
		case <-d.done:
			close(ch)
		}
	}()
	return stream
}
