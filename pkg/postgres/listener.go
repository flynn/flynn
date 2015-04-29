package postgres

import (
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
)

// Listen creates a listener for the given channel, returning the listener
// and the first connection error (nil on successful connection).
func (db *DB) Listen(channel string, log log15.Logger) (*Listener, error) {
	l := &Listener{
		Notify:   make(chan *pq.Notification),
		firstErr: make(chan error, 1),
		log:      log,
	}
	l.pqListener = pq.NewListener(db.DSN(), 10*time.Second, time.Minute, l.handleEvent)
	if err := l.pqListener.Listen(channel); err != nil {
		return nil, err
	}
	go l.listen()
	return l, <-l.firstErr
}

type Listener struct {
	Notify chan *pq.Notification
	Err    error

	firstErr   chan error
	firstOnce  sync.Once
	pqListener *pq.Listener
	closeOnce  sync.Once
	log        log15.Logger
}

func (l *Listener) Close() (err error) {
	l.closeOnce.Do(func() {
		err = l.pqListener.Close()
	})
	return
}

func (l *Listener) maybeFirstErr(err error) {
	l.firstOnce.Do(func() {
		l.firstErr <- err
		close(l.firstErr)
	})
}

func (l *Listener) handleEvent(ev pq.ListenerEventType, err error) {
	switch ev {
	case pq.ListenerEventConnected:
		l.log.Info("pq listener connected")
		l.maybeFirstErr(nil)
	case pq.ListenerEventDisconnected:
		l.log.Error("pq listener disconnected", "err", err)
		l.maybeFirstErr(err)
		l.Err = err
		l.Close()
	case pq.ListenerEventConnectionAttemptFailed:
		l.log.Error("pq listener connection attempt failed", "err", err)
		l.maybeFirstErr(err)
		l.Err = err
		l.Close()
	}
}

func (l *Listener) listen() {
	for {
		select {
		case n, ok := <-l.pqListener.Notify:
			if !ok {
				close(l.Notify)
				return
			}
			if n == nil { // reconnect
				continue
			}
			l.Notify <- n
		}
	}
}
