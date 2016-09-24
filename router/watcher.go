package main

import (
	"sync"

	"github.com/flynn/flynn/router/types"
)

type Watcher interface {
	Watch(ch chan *router.Event, sendCurrent bool)
	Unwatch(ch chan *router.Event)
}

func NewWatchManager() *WatchManager {
	return &WatchManager{
		watchers: make(map[chan *router.Event]struct{}),
		backends: make(map[string]map[string]*router.Backend),
	}
}

type WatchManager struct {
	mtx      sync.RWMutex
	watchers map[chan *router.Event]struct{}
	backends map[string]map[string]*router.Backend
}

func (m *WatchManager) Watch(ch chan *router.Event, sendCurrent bool) {
	m.mtx.Lock()
	if sendCurrent {
		for _, backends := range m.backends {
			for _, backend := range backends {
				ch <- &router.Event{
					Event:   router.EventTypeBackendUp,
					Backend: backend,
				}
			}
		}
	}
	m.watchers[ch] = struct{}{}
	m.mtx.Unlock()
}

func (m *WatchManager) Unwatch(ch chan *router.Event) {
	go func() {
		// drain channel so that we don't deadlock
		for range ch {
		}
	}()
	m.mtx.Lock()
	delete(m.watchers, ch)
	m.mtx.Unlock()
	close(ch)
}

func (m *WatchManager) Send(event *router.Event) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	switch event.Event {
	case router.EventTypeBackendUp:
		if m.backends[event.Backend.Service] == nil {
			m.backends[event.Backend.Service] = make(map[string]*router.Backend)
		}
		m.backends[event.Backend.Service][event.Backend.JobID] = event.Backend
	case router.EventTypeBackendDown:
		if backends, ok := m.backends[event.Backend.Service]; ok {
			delete(backends, event.Backend.JobID)
			if len(backends) == 0 {
				m.backends[event.Backend.Service] = nil
			}
		}
	case router.EventTypeRouteRemove:
		if backends, ok := m.backends[event.Route.Service]; ok {
			for _, backend := range backends {
				for ch := range m.watchers {
					ch <- &router.Event{
						Event:   router.EventTypeBackendDown,
						Backend: backend,
					}
				}
			}
			delete(m.backends, event.Route.Service)
		}
	}

	for ch := range m.watchers {
		ch <- event
	}
}
