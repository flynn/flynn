package queue

import (
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
)

// listener is an internal wrapper around *pq.Listener which dispatches notifications from PostgreSQL.
type listener struct {
	l         *pq.Listener
	err       chan error
	mtx       sync.RWMutex
	listeners map[string]map[chan *notification]struct{}
}

type notification struct {
	extra string
	err   error
}

func newListener(db *sql.DB) *listener {
	l := &listener{
		err:       make(chan error),
		listeners: make(map[string]map[chan *notification]struct{}),
	}
	l.l = pq.NewListener(db.DSN(), 10*time.Second, time.Minute, l.handleEvent)
	go l.start()
	return l
}

func (l *listener) handleEvent(ev pq.ListenerEventType, err error) {
	if ev == pq.ListenerEventConnectionAttemptFailed {
		l.err <- err
	}
}

func (l *listener) start() {
	for {
		select {
		case err := <-l.err:
			l.notifyErr(err)
			return
		case n := <-l.l.Notify:
			l.notify(n.Channel, &notification{extra: n.Extra})
		}
	}
}

func (l *listener) listen(channel string) (chan *notification, error) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if _, ok := l.listeners[channel]; !ok {
		if err := l.l.Listen(channel); err != nil {
			return nil, err
		}
		l.listeners[channel] = make(map[chan *notification]struct{})
	}
	ch := make(chan *notification)
	l.listeners[channel][ch] = struct{}{}
	return ch, nil
}

func (l *listener) unlisten(channel string, ch chan *notification) {
	go func() {
		// drain to prevent deadlock while removing the listener
		for range ch {
		}
	}()
	l.mtx.Lock()
	delete(l.listeners[channel], ch)
	if len(l.listeners[channel]) == 0 {
		l.l.Unlisten(channel)
		delete(l.listeners, channel)
	}
	l.mtx.Unlock()
	close(ch)
}

func (l *listener) notify(channel string, n *notification) {
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	if listeners, ok := l.listeners[channel]; ok {
		for ch := range listeners {
			ch <- n
		}
	}
}

func (l *listener) notifyErr(err error) {
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	for _, listeners := range l.listeners {
		for ch := range listeners {
			ch <- &notification{err: err}
		}
	}
}
