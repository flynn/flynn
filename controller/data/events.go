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
	query := "SELECT event_id, app_id, deployment_id, object_id, object_type, data, op, created_at FROM events"
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

type ListEventOptions struct {
	PageToken     PageToken
	AppIDs        []string
	DeploymentIDs []string
	ObjectTypes   []ct.EventType
}

func (r *EventRepo) ListPage(opts ListEventOptions) ([]*ct.ExpandedEvent, *PageToken, error) {
	pageSize := DEFAULT_PAGE_SIZE
	if opts.PageToken.Size > 0 {
		pageSize = opts.PageToken.Size
	}
	objectTypes := make([]string, len(opts.ObjectTypes))
	for i, t := range opts.ObjectTypes {
		objectTypes[i] = string(t)
	}
	cursor, err := opts.PageToken.Cursor()
	if err != nil {
		return nil, nil, err
	}
	rows, err := r.db.Query("event_list_page", cursor, opts.AppIDs, opts.DeploymentIDs, objectTypes, pageSize+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var events []*ct.ExpandedEvent
	for rows.Next() {
		event, err := scanExpandedEvent(rows)
		if err != nil {
			return nil, nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var nextPageToken *PageToken
	if len(events) == pageSize+1 {
		nextPageToken = &PageToken{
			CursorID: toCursorID(events[pageSize].CreatedAt),
			Size:     pageSize,
		}
		events = events[0:pageSize]
	}
	return events, nextPageToken, nil
}

func (r *EventRepo) GetEvent(id int64) (*ct.Event, error) {
	row := r.db.QueryRow("event_select", id)
	return scanEvent(row)
}

func (r *EventRepo) GetExpandedEvent(id int64) (*ct.ExpandedEvent, error) {
	row := r.db.QueryRow("event_select_expanded", id)
	return scanExpandedEvent(row)
}

func scanEvent(s postgres.Scanner) (*ct.Event, error) {
	var event ct.Event
	var typ string
	var data []byte
	var appID *string
	var deploymentID *string
	var op *string
	err := s.Scan(&event.ID, &appID, &deploymentID, &event.ObjectID, &typ, &data, &op, &event.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	if appID != nil {
		event.AppID = *appID
	}
	if deploymentID != nil {
		event.DeploymentID = *deploymentID
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

func scanExpandedEvent(s postgres.Scanner) (*ct.ExpandedEvent, error) {
	// Event
	var event ct.ExpandedEvent
	var typ string
	var appID *string
	var data []byte
	var op *string

	// ExpandedDeployment
	d := &ct.ExpandedDeployment{}
	var deploymentID *string
	var deploymentAppID *string
	var deploymentStrategy *string
	var deploymentStatus *string
	var deploymentType *ct.ReleaseType
	var deploymentTimeout *int32
	oldRelease := &ct.Release{}
	newRelease := &ct.Release{}
	var oldArtifactIDs *string
	var newArtifactIDs *string
	var oldReleaseID *string
	var newReleaseID *string

	// Job
	job := &ct.Job{}
	var jobID *string
	var jobUUID *string
	var jobHostID *string
	var jobAppID *string
	var jobReleaseID *string
	var jobType *string
	var jobState *string
	var jobVolumeIDs *string

	// ScaleRequest
	sr := &ct.ScaleRequest{}
	var srID *string
	var srAppID *string
	var srReleaseID *string
	var srState *ct.ScaleRequestState
	var srOldProcesses *map[string]int
	var srOldTags *map[string]map[string]string

	err := s.Scan(
		// Event
		&event.ID, &appID, &event.ObjectID, &typ, &data, &op, &event.CreatedAt,

		// ExpandedDeployment
		&deploymentID, &deploymentAppID, &oldReleaseID, &newReleaseID, &deploymentStrategy, &deploymentStatus, &d.Processes, &d.Tags, &deploymentTimeout, &d.DeployBatchSize, &d.CreatedAt, &d.FinishedAt,
		&oldArtifactIDs, &oldRelease.Env, &oldRelease.Processes, &oldRelease.Meta, &oldRelease.CreatedAt,
		&newArtifactIDs, &newRelease.Env, &newRelease.Processes, &newRelease.Meta, &newRelease.CreatedAt,
		&deploymentType,

		// Job
		&jobID, &jobUUID, &jobHostID, &jobAppID, &jobReleaseID, &jobType, &jobState, &job.Meta, &job.ExitStatus, &job.HostError, &job.RunAt, &job.Restarts, &job.CreatedAt, &job.UpdatedAt, &job.Args, &jobVolumeIDs,

		// ScaleRequest
		&srID, &srAppID, &srReleaseID, &srState, &srOldProcesses, &sr.NewProcesses, &srOldTags, &sr.NewTags, &sr.CreatedAt, &sr.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}

	// Event
	if appID != nil {
		event.AppID = *appID
	}
	if op != nil {
		event.Op = ct.EventOp(*op)
	}
	event.ObjectType = ct.EventType(typ)
	event.Data = json.RawMessage(data)

	// ExpandedDeployment
	if deploymentID != nil {
		d.ID = *deploymentID
		sr.DeploymentID = d.ID
		job.DeploymentID = d.ID
	}
	if deploymentAppID != nil {
		d.AppID = *deploymentAppID
	}
	if oldReleaseID != nil {
		oldRelease.ID = *oldReleaseID
		oldRelease.AppID = d.AppID
		if oldArtifactIDs != nil && *oldArtifactIDs != "" {
			oldRelease.ArtifactIDs = splitPGStringArray(*oldArtifactIDs)
		}
		d.OldRelease = oldRelease
	}
	if newReleaseID != nil {
		newRelease.ID = *newReleaseID
		newRelease.AppID = d.AppID
		if newArtifactIDs != nil && *newArtifactIDs != "" {
			newRelease.ArtifactIDs = splitPGStringArray(*newArtifactIDs)
		}
		d.NewRelease = newRelease
	}
	if deploymentStrategy != nil {
		d.Strategy = *deploymentStrategy
	}
	if deploymentStatus != nil {
		d.Status = *deploymentStatus
	}
	if deploymentType != nil {
		d.Type = *deploymentType
	}
	if deploymentTimeout != nil {
		d.DeployTimeout = *deploymentTimeout
	}

	// Job
	if jobID != nil {
		job.ID = *jobID
	}
	if jobUUID != nil {
		job.UUID = *jobUUID
	}
	if jobHostID != nil {
		job.HostID = *jobHostID
	}
	if jobAppID != nil {
		job.AppID = *jobAppID
	}
	if jobReleaseID != nil {
		job.ReleaseID = *jobReleaseID
	}
	if jobType != nil {
		job.Type = *jobType
	}
	if jobState != nil {
		job.State = ct.JobState(*jobState)
	}
	if jobVolumeIDs != nil && *jobVolumeIDs != "" {
		job.VolumeIDs = splitPGStringArray(*jobVolumeIDs)
	}

	if d.ID != "" {
		event.Deployment = d
	}

	if job.UUID != "" {
		event.Job = job
	}

	// ScaleRequest
	if srID != nil {
		sr.ID = *srID
		event.ScaleRequest = sr
	}
	if srAppID != nil {
		sr.AppID = *srAppID
	}
	if srReleaseID != nil {
		sr.ReleaseID = *srReleaseID
	}
	if srState != nil {
		sr.State = ct.ScaleRequestState(*srState)
	}
	if srOldProcesses != nil {
		sr.OldProcesses = *srOldProcesses
	}
	if srOldTags != nil {
		sr.OldTags = *srOldTags
	}

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
	Events  chan *ct.ExpandedEvent
	Err     error
	errOnce sync.Once

	l             *EventListener
	queue         chan *ct.ExpandedEvent
	appIDs        []string
	deploymentIDs []string
	objectTypes   []string
	objectIDs     []string

	stop     chan struct{}
	stopOnce sync.Once
}

// Notify filters the event based on it's appID, type and objectID and then
// pushes it to the event queue.
func (e *EventSubscriber) Notify(event *ct.ExpandedEvent) {
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
	if len(e.deploymentIDs) > 0 {
		matchesID := false
		if event.Deployment != nil {
			for _, deploymentID := range e.deploymentIDs {
				if deploymentID == event.Deployment.ID {
					matchesID = true
					break
				}
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

type EventSubscriptionOpts struct {
	AppIDs        []string
	DeploymentIDs []string
	ObjectTypes   []ct.EventType
	ObjectIDs     []string
}

// Subscribe creates and returns an EventSubscriber for the given apps, types and objects.
// An empty appIDs list subscribes to all apps
func (e *EventListener) Subscribe(appIDs, objectTypes, objectIDs []string) (*EventSubscriber, error) {
	ctObjectTypes := make([]ct.EventType, len(objectTypes))
	for i, t := range objectTypes {
		ctObjectTypes[i] = ct.EventType(t)
	}
	return e.SubscribeWithOpts(&EventSubscriptionOpts{
		AppIDs:      appIDs,
		ObjectTypes: ctObjectTypes,
		ObjectIDs:   objectIDs,
	})
}

// SubscribeWithOpts creates and returns an EventSubscriber for the given opts.
// An empty ID list subscribes to all
func (e *EventListener) SubscribeWithOpts(opts *EventSubscriptionOpts) (*EventSubscriber, error) {
	e.subMtx.Lock()
	defer e.subMtx.Unlock()
	if e.IsClosed() {
		return nil, errors.New("event listener closed")
	}
	objectTypeStrings := make([]string, len(opts.ObjectTypes))
	for i, t := range opts.ObjectTypes {
		objectTypeStrings[i] = string(t)
	}
	s := &EventSubscriber{
		Events:        make(chan *ct.ExpandedEvent),
		l:             e,
		queue:         make(chan *ct.ExpandedEvent, eventBufferSize),
		stop:          make(chan struct{}),
		appIDs:        opts.AppIDs,
		deploymentIDs: opts.DeploymentIDs,
		objectTypes:   objectTypeStrings,
		objectIDs:     opts.ObjectIDs,
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
				event, err := e.eventRepo.GetExpandedEvent(id)
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
func (e *EventListener) Notify(event *ct.ExpandedEvent) {
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
