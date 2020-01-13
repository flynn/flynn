package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/jackc/pgx"
)

type EventRepo struct {
	db *postgres.DB
}

func NewEventRepo(db *postgres.DB) *EventRepo {
	return &EventRepo{db: db}
}

func (r *EventRepo) ListEvents(appIDs, objectTypes, objectIDs []string, beforeID *int64, sinceID *int64, count int) ([]*ct.Event, error) {
	query := "SELECT event_id, app_id, object_id, object_type, data, op, created_at FROM events"
	var conditions []string
	var n int
	args := []interface{}{}
	if beforeID != nil {
		n++
		conditions = append(conditions, fmt.Sprintf("event_id < $%d", n))
		args = append(args, *beforeID)
	}
	if sinceID != nil {
		n++
		conditions = append(conditions, fmt.Sprintf("event_id > $%d", n))
		args = append(args, *sinceID)
	}
	if len(appIDs) > 0 {
		n++
		conditions = append(conditions, fmt.Sprintf("app_id::text = ANY($%d::text[])", n))
		args = append(args, appIDs)
	}
	if len(objectTypes) > 0 {
		c := "("
		for i, typ := range objectTypes {
			if i > 0 {
				c += " OR "
			}
			n++
			c += fmt.Sprintf("object_type = $%d", n)
			args = append(args, typ)
		}
		c += ")"
		conditions = append(conditions, c)
	}
	if len(objectIDs) > 0 {
		n++
		conditions = append(conditions, fmt.Sprintf("object_id = ANY($%d::text[])", n))
		args = append(args, objectIDs)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY event_id DESC"
	if count > 0 {
		n++
		query += fmt.Sprintf(" LIMIT $%d", n)
		args = append(args, count)
	}
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*ct.Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *EventRepo) GetEvent(id int64) (*ct.Event, error) {
	row := r.db.QueryRow("event_select", id)
	return scanEvent(row)
}

func scanEvent(s postgres.Scanner) (*ct.Event, error) {
	var event ct.Event
	var typ string
	var data []byte
	var appID *string
	var op *string
	err := s.Scan(&event.ID, &appID, &event.ObjectID, &typ, &data, &op, &event.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	if appID != nil {
		event.AppID = *appID
	}
	if data == nil {
		data = []byte("null")
	}
	if op != nil {
		event.Op = ct.EventOp(*op)
	}
	event.ObjectType = ct.EventType(typ)
	event.Data = json.RawMessage(data)
	return &event, nil
}

// ErrEventBufferOverflow is returned to clients when the in-memory event
// buffer is full due to clients not reading events quickly enough.
var ErrEventBufferOverflow = errors.New("event stream buffer overflow")

// eventBufferSize is the amount of events to buffer in memory.
const eventBufferSize = 1000

// EventSubscriber receives events from the EventListener loop and maintains
// it's own loop to forward those events to the Events channel.
type EventSubscriber struct {
	Events  chan *ct.Event
	Err     error
	errOnce sync.Once

	l           *EventListener
	queue       chan *ct.Event
	appIDs      []string
	objectTypes []string
	objectIDs   []string

	stop     chan struct{}
	stopOnce sync.Once
}

// Notify filters the event based on it's appID, type and objectID and then
// pushes it to the event queue.
func (e *EventSubscriber) Notify(event *ct.Event) {
	if len(e.appIDs) > 0 {
		matchesApp := false
		for _, appID := range e.appIDs {
			if event.AppID == appID {
				matchesApp = true
				break
			}
		}
		if !matchesApp {
			return
		}
	}
	if len(e.objectTypes) > 0 {
		matchesType := false
		for _, typ := range e.objectTypes {
			if typ == string(event.ObjectType) {
				matchesType = true
				break
			}
		}
		if !matchesType {
			return
		}
	}
	if len(e.objectIDs) > 0 {
		matchesID := false
		for _, objectID := range e.objectIDs {
			if objectID == event.ObjectID {
				matchesID = true
				break
			}
		}
		if !matchesID {
			return
		}
	}
	select {
	case e.queue <- event:
	default:
		// Run in a goroutine to avoid deadlock with Notify
		go e.CloseWithError(ErrEventBufferOverflow)
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
	e.errOnce.Do(func() { e.Err = err })
	e.Close()
}

func NewEventListener(r *EventRepo) *EventListener {
	return &EventListener{
		eventRepo:   r,
		subscribers: make(map[*EventSubscriber]struct{}),
		doneCh:      make(chan struct{}),
	}
}

// EventListener creates a postgres Listener for events and forwards them
// to subscribers.
type EventListener struct {
	eventRepo *EventRepo

	subscribers map[*EventSubscriber]struct{}
	subMtx      sync.RWMutex

	closed    bool
	closedMtx sync.RWMutex
	doneCh    chan struct{}
}

// Subscribe creates and returns an EventSubscriber for the given apps, types and objects.
// An empty appIDs list subscribes to all apps
func (e *EventListener) Subscribe(appIDs, objectTypes, objectIDs []string) (*EventSubscriber, error) {
	e.subMtx.Lock()
	defer e.subMtx.Unlock()
	if e.IsClosed() {
		return nil, errors.New("event listener closed")
	}
	s := &EventSubscriber{
		Events:      make(chan *ct.Event),
		l:           e,
		queue:       make(chan *ct.Event, eventBufferSize),
		stop:        make(chan struct{}),
		appIDs:      appIDs,
		objectTypes: objectTypes,
		objectIDs:   objectIDs,
	}
	go s.loop()
	e.subscribers[s] = struct{}{}
	return s, nil
}

// Unsubscribe unsubscribes the given subscriber.
func (e *EventListener) Unsubscribe(s *EventSubscriber) {
	e.subMtx.Lock()
	defer e.subMtx.Unlock()
	delete(e.subscribers, s)
}

// Listen creates a postgres listener for events and starts a goroutine to
// forward the events to subscribers.
func (e *EventListener) Listen() error {
	log := logger.New("fn", "EventListener.Listen")
	listener, err := e.eventRepo.db.Listen("events", log)
	if err != nil {
		e.CloseWithError(err)
		return err
	}
	go func() {
		for {
			select {
			case n, ok := <-listener.Notify:
				if !ok {
					e.CloseWithError(listener.Err)
					return
				}
				idApp := strings.SplitN(n.Payload, ":", 2)
				if len(idApp) < 1 {
					log.Error(fmt.Sprintf("invalid event notification: %q", n.Payload))
					continue
				}
				id, err := strconv.ParseInt(idApp[0], 10, 64)
				if err != nil {
					log.Error(fmt.Sprintf("invalid event notification: %q", n.Payload), "err", err)
					continue
				}
				event, err := e.eventRepo.GetEvent(id)
				if err != nil {
					log.Error(fmt.Sprintf("invalid event notification: %q", n.Payload), "err", err)
					continue
				}
				e.Notify(event)
			case <-e.doneCh:
				listener.Close()
				return
			}
		}
	}()
	return nil
}

// Notify notifies all sbscribers of the given event.
func (e *EventListener) Notify(event *ct.Event) {
	e.subMtx.RLock()
	defer e.subMtx.RUnlock()
	for sub := range e.subscribers {
		sub.Notify(event)
	}
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
	e.closedMtx.Lock()
	if e.closed {
		e.closedMtx.Unlock()
		return
	}
	e.closed = true
	e.closedMtx.Unlock()

	e.subMtx.RLock()
	defer e.subMtx.RUnlock()
	for sub := range e.subscribers {
		go sub.CloseWithError(err)
	}
	close(e.doneCh)
}
