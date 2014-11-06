package main

import (
	"sync"

	"github.com/flynn/flynn/router/types"
)

type Watcher interface {
	Watch(chan *router.Event)
	Unwatch(chan *router.Event)
}

func NewWatchManager() *WatchManager {
	return &WatchManager{watchers: make(map[chan *router.Event]struct{})}
}

type WatchManager struct {
	mtx      sync.RWMutex
	watchers map[chan *router.Event]struct{}
}

func (m *WatchManager) Watch(ch chan *router.Event) {
	m.mtx.Lock()
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
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	for ch := range m.watchers {
		ch <- event
	}
}
