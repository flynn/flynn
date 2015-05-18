package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	ct "github.com/flynn/flynn/controller/types"
)

// ErrEventBufferOverflow is returned to clients when the in-memory event
// buffer is full due to clients not reading events quickly enough.
var ErrEventBufferOverflow = errors.New("event stream buffer overflow")

// eventBufferSize is the amount of events to buffer in memory.
const eventBufferSize = 1000

// EventSubscriber receives app events from the EventListener loop and maintains
// it's own loop to forward those events to the Events channel.
type EventSubscriber struct {
	Events chan *ct.AppEvent
	Err    error

	l          *EventListener
	queue      chan *ct.AppEvent
	appID      string
	objectType string
	objectID   string

	stop     chan struct{}
	stopOnce sync.Once
}

// Notify filters the event based on it's type and objectID and then pushes
// it to the event queue.
func (e *EventSubscriber) Notify(event *ct.AppEvent) {
	if e.objectType != "" && e.objectType != string(event.ObjectType) {
		return
	}
	if e.objectID != "" && e.objectID != event.ObjectID {
		return
	}
	select {
	case e.queue <- event:
	default:
		e.CloseWithError(ErrEventBufferOverflow)
	}
}

// loop pops events off the queue and sends them to the Events channel.
func (e *EventSubscriber) loop() {
	defer close(e.Events)
	for {
		select {
		case <-e.stop:
			return
		case event := <-e.queue:
			e.Events <- event
		}
	}
}

// Close unsubscribes from the EventListener and stops the loop.
func (e *EventSubscriber) Close() {
	e.l.Unsubscribe(e)
	e.stopOnce.Do(func() { close(e.stop) })
}

// CloseWithError sets the Err field and then closes the subscriber.
func (e *EventSubscriber) CloseWithError(err error) {
	e.Err = err
	e.Close()
}

func newEventListener(a *AppRepo) *EventListener {
	return &EventListener{
		appRepo:     a,
		subscribers: make(map[string]map[*EventSubscriber]struct{}),
	}
}

// EventListener creates a postgres Listener for app events and forwards them
// to subscribers.
type EventListener struct {
	appRepo *AppRepo

	subscribers map[string]map[*EventSubscriber]struct{}
	subMtx      sync.RWMutex

	closed    bool
	closedMtx sync.RWMutex
}

// Subscribe creates and returns an EventSubscriber for the given app, type and object.
func (e *EventListener) Subscribe(appID, objectType, objectID string) (*EventSubscriber, error) {
	e.subMtx.Lock()
	defer e.subMtx.Unlock()
	if e.IsClosed() {
		return nil, errors.New("event listener closed")
	}
	s := &EventSubscriber{
		Events:     make(chan *ct.AppEvent),
		l:          e,
		queue:      make(chan *ct.AppEvent, eventBufferSize),
		stop:       make(chan struct{}),
		appID:      appID,
		objectType: objectType,
		objectID:   objectID,
	}
	go s.loop()
	if _, ok := e.subscribers[appID]; !ok {
		e.subscribers[appID] = make(map[*EventSubscriber]struct{})
	}
	e.subscribers[appID][s] = struct{}{}
	return s, nil
}

// Unsubscribe unsubscribes the given subscriber.
func (e *EventListener) Unsubscribe(s *EventSubscriber) {
	e.subMtx.Lock()
	defer e.subMtx.Unlock()
	if subs, ok := e.subscribers[s.appID]; ok {
		delete(subs, s)
		if len(subs) == 0 {
			delete(e.subscribers, s.appID)
		}
	}
}

// Listen creates a postgres listener for app events and starts a goroutine to
// forward the events to subscribers.
func (e *EventListener) Listen() error {
	log := log15.New("component", "controller", "fn", "EventListener.Listen")
	listener, err := e.appRepo.db.Listen("app_events", log)
	if err != nil {
		e.SetClosed()
		return err
	}
	go func() {
		for {
			n, ok := <-listener.Notify
			if !ok {
				e.CloseWithError(listener.Err)
				return
			}
			idApp := strings.SplitN(n.Extra, ":", 2)
			if len(idApp) != 2 {
				log.Error(fmt.Sprintf("invalid app event notification: %q", n.Extra))
				continue
			}
			id, err := strconv.ParseInt(idApp[0], 10, 64)
			if err != nil {
				log.Error(fmt.Sprintf("invalid app event notification: %q", n.Extra), "err", err)
				continue
			}
			event, err := e.appRepo.GetEvent(id)
			if err != nil {
				log.Error(fmt.Sprintf("invalid app event notification: %q", n.Extra), "err", err)
				continue
			}
			e.Notify(event)
		}
	}()
	return nil
}

// Notify notifies all sbscribers of the given event.
func (e *EventListener) Notify(event *ct.AppEvent) {
	e.subMtx.RLock()
	subscribers := e.subscribers
	e.subMtx.RUnlock()
	if subs, ok := subscribers[event.AppID]; ok {
		for sub := range subs {
			sub.Notify(event)
		}
	}
}

// SetClosed marks the listener as closed.
func (e *EventListener) SetClosed() {
	e.closedMtx.Lock()
	defer e.closedMtx.Unlock()
	e.closed = true
}

// IsClosed returns whether or not the listener is closed.
func (e *EventListener) IsClosed() bool {
	e.closedMtx.RLock()
	defer e.closedMtx.RUnlock()
	return e.closed
}

// CloseWithError marks the listener as closed and closes all subscribers
// with the given error.
func (e *EventListener) CloseWithError(err error) {
	e.SetClosed()
	e.subMtx.RLock()
	subscribers := e.subscribers
	e.subMtx.RUnlock()
	for _, subs := range subscribers {
		for sub := range subs {
			sub.CloseWithError(err)
		}
	}
}
