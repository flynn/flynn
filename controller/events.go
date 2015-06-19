package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/sse"
)

type EventRepo struct {
	db *postgres.DB
}

func NewEventRepo(db *postgres.DB) *EventRepo {
	return &EventRepo{db: db}
}

func (r *EventRepo) ListEvents(appID string, objectTypes []string, objectID string, sinceID int64, count int) ([]*ct.Event, error) {
	query := "SELECT event_id, app_id, object_id, object_type, data, created_at FROM events WHERE event_id > $1"
	args := []interface{}{sinceID}
	n := 2
	if appID != "" {
		query += fmt.Sprintf(" AND app_id = $%d", n)
		n++
		args = append(args, appID)
	}
	if len(objectTypes) > 0 {
		query += " AND ("
		for i, typ := range objectTypes {
			if i > 0 {
				query += " OR "
			}
			query += fmt.Sprintf("object_type = $%d", n)
			n++
			args = append(args, typ)
		}
		query += ")"
	}
	if objectID != "" {
		query += fmt.Sprintf(" AND object_id = $%d", n)
		args = append(args, objectID)
	}
	query += " ORDER BY event_id DESC"
	if count > 0 {
		query += fmt.Sprintf(" LIMIT $%d", n)
		args = append(args, count)
	}
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	var events []*ct.Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (r *EventRepo) GetEvent(id int64) (*ct.Event, error) {
	row := r.db.QueryRow("SELECT event_id, app_id, object_id, object_type, data, created_at FROM events WHERE event_id = $1", id)
	return scanEvent(row)
}

func scanEvent(s postgres.Scanner) (*ct.Event, error) {
	var event ct.Event
	var typ string
	var data []byte
	var appID *string
	err := s.Scan(&event.ID, &appID, &event.ObjectID, &typ, &data, &event.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	if appID != nil {
		event.AppID = postgres.CleanUUID(*appID)
	}
	event.ObjectType = ct.EventType(typ)
	event.Data = json.RawMessage(data)
	return &event, nil
}

func (c *controllerAPI) maybeStartEventListener() error {
	c.eventListenerMtx.Lock()
	defer c.eventListenerMtx.Unlock()
	if c.eventListener != nil && !c.eventListener.IsClosed() {
		return nil
	}
	c.eventListener = newEventListener(c.eventRepo)
	return c.eventListener.Listen()
}

func (c *controllerAPI) Events(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	if err := c.maybeStartEventListener(); err != nil {
		respondWithError(w, err)
	}
	if err := streamEvents(ctx, w, req, c.eventListener, c.maybeGetApp(ctx), c.eventRepo); err != nil {
		respondWithError(w, err)
	}
}

func streamEvents(ctx context.Context, w http.ResponseWriter, req *http.Request, eventListener *EventListener, app *ct.App, repo *EventRepo) (err error) {
	var appID string
	if app != nil {
		appID = app.ID
	}

	var lastID int64
	if req.Header.Get("Last-Event-Id") != "" {
		lastID, err = strconv.ParseInt(req.Header.Get("Last-Event-Id"), 10, 64)
		if err != nil {
			return ct.ValidationError{Field: "Last-Event-Id", Message: "is invalid"}
		}
	}

	var count int
	if req.FormValue("count") != "" {
		count, err = strconv.Atoi(req.FormValue("count"))
		if err != nil {
			return ct.ValidationError{Field: "count", Message: "is invalid"}
		}
	}

	objectTypes := strings.Split(req.FormValue("object_types"), ",")
	if len(objectTypes) == 1 && objectTypes[0] == "" {
		objectTypes = []string{}
	}
	objectID := req.FormValue("object_id")
	past := req.FormValue("past")

	l, _ := ctxhelper.LoggerFromContext(ctx)
	log := l.New("fn", "Events", "object_types", objectTypes, "object_id", objectID)
	ch := make(chan *ct.Event)
	s := sse.NewStream(w, ch, log)
	s.Serve()
	defer func() {
		if err == nil {
			s.Close()
		} else {
			s.CloseWithError(err)
		}
	}()

	sub, err := eventListener.Subscribe(appID, objectTypes, objectID)
	if err != nil {
		return err
	}
	defer sub.Close()

	var currID int64
	if past == "true" || lastID > 0 {
		list, err := repo.ListEvents(appID, objectTypes, objectID, lastID, count)
		if err != nil {
			return err
		}
		// events are in ID DESC order, so iterate in reverse
		for i := len(list) - 1; i >= 0; i-- {
			e := list[i]
			ch <- e
			currID = e.ID
		}
	}

	for {
		select {
		case <-s.Done:
			return
		case event, ok := <-sub.Events:
			if !ok {
				return sub.Err
			}
			if event.ID <= currID {
				continue
			}
			ch <- event
		}
	}
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
	appID       string
	objectTypes []string
	objectID    string

	stop     chan struct{}
	stopOnce sync.Once
}

// Notify filters the event based on it's type and objectID and then pushes
// it to the event queue.
func (e *EventSubscriber) Notify(event *ct.Event) {
	if len(e.objectTypes) > 0 {
		foundType := false
		for _, typ := range e.objectTypes {
			if typ == string(event.ObjectType) {
				foundType = true
				break
			}
		}
		if !foundType {
			return
		}
	}
	if e.objectID != "" && e.objectID != event.ObjectID {
		return
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

func newEventListener(r *EventRepo) *EventListener {
	return &EventListener{
		eventRepo:   r,
		subscribers: make(map[string]map[*EventSubscriber]struct{}),
	}
}

// EventListener creates a postgres Listener for events and forwards them
// to subscribers.
type EventListener struct {
	eventRepo *EventRepo

	subscribers map[string]map[*EventSubscriber]struct{}
	subMtx      sync.RWMutex

	closed    bool
	closedMtx sync.RWMutex
}

// Subscribe creates and returns an EventSubscriber for the given app, type and object.
// Using an empty string for appID subscribes to all apps
func (e *EventListener) Subscribe(appID string, objectTypes []string, objectID string) (*EventSubscriber, error) {
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
		appID:       appID,
		objectTypes: objectTypes,
		objectID:    objectID,
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

// Listen creates a postgres listener for events and starts a goroutine to
// forward the events to subscribers.
func (e *EventListener) Listen() error {
	log := log15.New("component", "controller", "fn", "EventListener.Listen")
	listener, err := e.eventRepo.db.Listen("events", log)
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
			if len(idApp) < 1 {
				log.Error(fmt.Sprintf("invalid event notification: %q", n.Extra))
				continue
			}
			id, err := strconv.ParseInt(idApp[0], 10, 64)
			if err != nil {
				log.Error(fmt.Sprintf("invalid event notification: %q", n.Extra), "err", err)
				continue
			}
			event, err := e.eventRepo.GetEvent(id)
			if err != nil {
				log.Error(fmt.Sprintf("invalid event notification: %q", n.Extra), "err", err)
				continue
			}
			e.Notify(event)
		}
	}()
	return nil
}

// Notify notifies all sbscribers of the given event.
func (e *EventListener) Notify(event *ct.Event) {
	e.subMtx.RLock()
	defer e.subMtx.RUnlock()
	if subs, ok := e.subscribers[event.AppID]; ok {
		for sub := range subs {
			sub.Notify(event)
		}
	}
	if event.AppID != "" {
		// Ensure subscribers not filtering by app get the event
		if subs, ok := e.subscribers[""]; ok {
			for sub := range subs {
				sub.Notify(event)
			}
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
	defer e.subMtx.RUnlock()
	subscribers := e.subscribers
	for _, subs := range subscribers {
		for sub := range subs {
			go sub.CloseWithError(err)
		}
	}
}
