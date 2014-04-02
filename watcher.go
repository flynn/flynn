package main

import (
	"sync"

	"github.com/flynn/strowger/types"
)

type Watcher interface {
	Watch(chan *strowger.Event)
	Unwatch(chan *strowger.Event)
}

func NewWatchManager() *WatchManager {
	return &WatchManager{watchers: make(map[chan *strowger.Event]struct{})}
}

type WatchManager struct {
	mtx      sync.RWMutex
	watchers map[chan *strowger.Event]struct{}
}

func (m *WatchManager) Watch(ch chan *strowger.Event) {
	m.mtx.Lock()
	m.watchers[ch] = struct{}{}
	m.mtx.Unlock()
}

func (m *WatchManager) Unwatch(ch chan *strowger.Event) {
	go func() {
		// drain channel so that we don't deadlock
		for _ = range ch {
		}
	}()
	m.mtx.Lock()
	delete(m.watchers, ch)
	m.mtx.Unlock()
}

func (m *WatchManager) Send(event *strowger.Event) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	for ch := range m.watchers {
		ch <- event
	}
}
