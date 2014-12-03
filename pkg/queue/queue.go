package queue

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
)

type Handler func(*Job) error

type Queue struct {
	DB    *sql.DB
	Table string

	mtx          sync.Mutex
	l            *listener
	stopListener chan struct{}
	subscribers  map[string]map[chan *Event]struct{}
	subscribeMtx sync.RWMutex
	waiters      map[string]chan error
	waitMtx      sync.Mutex
}

func New(db *sql.DB, table string) *Queue {
	q := &Queue{
		DB:           db,
		Table:        table,
		stopListener: make(chan struct{}),
		subscribers:  make(map[string]map[chan *Event]struct{}),
		waiters:      make(map[string]chan error),
	}
	go q.unlockJobsOfDeadWorkers()
	return q
}

const defaultMaxAttempts = 25

func (q *Queue) Push(name string, data []byte) (*Job, error) {
	return q.PushWithMaxAttempts(name, data, defaultMaxAttempts)
}

func (q *Queue) PushWithMaxAttempts(name string, data []byte, maxAttempts int) (*Job, error) {
	job := &Job{}
	return job, q.DB.QueryRow(fmt.Sprintf("INSERT INTO %s (q_name, data, max_attempts) VALUES ($1, $2, $3) RETURNING id, created_at", q.Table), name, data, maxAttempts).Scan(&job.ID, &job.CreatedAt)
}

func (q *Queue) Get(id int64) (*Job, error) {
	job := &Job{}
	return job, q.DB.QueryRow(fmt.Sprintf("SELECT id, q_name, data, error_message, attempts, max_attempts, created_at FROM %s WHERE id = $1", q.Table), id).Scan(&job.ID, &job.Name, &job.Data, &job.ErrorMessage, &job.Attempts, &job.MaxAttempts, &job.CreatedAt)
}

func (q *Queue) NewWorker(name string, concurrency int, h Handler) *Worker {
	return newWorker(q, name, concurrency, h)
}

// Wait listens for job notify triggers for the given name, and returns a channel which will
// receive nil for a notification, or an error if a listen error occurs.
func (q *Queue) Wait(name string) chan error {
	q.waitMtx.Lock()
	defer q.waitMtx.Unlock()
	ch, ok := q.waiters[name]
	if ok {
		return ch
	}
	ch = make(chan error)
	q.waiters[name] = ch
	go func() {
		handleErr := func(err error) {
			ch <- err
			q.waitMtx.Lock()
			delete(q.waiters, name)
			q.waitMtx.Unlock()
			close(ch)
		}
		notify, err := q.listener().listen(name)
		if err != nil {
			handleErr(err)
			return
		}
		for n := range notify {
			if n.err != nil {
				handleErr(n.err)
				return
			}
			ch <- nil
		}
	}()
	return ch
}

func (q *Queue) unlockJobsOfDeadWorkers() {
	q.DB.Exec(fmt.Sprintf("UPDATE %s SET locked_at = NULL, locked_by = NULL WHERE locked_by NOT IN (SELECT pid FROM pg_stat_activity)", q.Table))
}

func (q *Queue) listener() *listener {
	q.mtx.Lock()
	defer q.mtx.Unlock()
	if q.l == nil {
		q.l = newListener(q.DB)
	}
	return q.l
}

type Event struct {
	ID        int64
	Job       *Job
	State     JobState
	CreatedAt *time.Time
}

type JobState uint8

func (s JobState) String() string {
	return map[JobState]string{
		JobStateRunning: "running",
		JobStateDone:    "done",
		JobStateError:   "error",
		JobStateFailed:  "failed",
	}[s]
}

const (
	JobStateRunning JobState = iota
	JobStateDone
	JobStateError
	JobStateFailed
)

func (q *Queue) Subscribe(name string) (chan *Event, error) {
	ch := make(chan *Event)
	var startListener bool
	q.subscribeMtx.Lock()
	if len(q.subscribers) == 0 {
		startListener = true
	}
	if _, ok := q.subscribers[name]; !ok {
		q.subscribers[name] = make(map[chan *Event]struct{})
	}
	q.subscribers[name][ch] = struct{}{}
	q.subscribeMtx.Unlock()
	if startListener {
		if err := q.startEventListener(); err != nil {
			return nil, err
		}
	}
	return ch, nil
}

func (q *Queue) Unsubscribe(name string, ch chan *Event) {
	go func() {
		// drain to prevent deadlock while removing the listener
		for range ch {
		}
	}()
	q.subscribeMtx.Lock()
	delete(q.subscribers[name], ch)
	if len(q.subscribers[name]) == 0 {
		delete(q.subscribers, name)
	}
	if len(q.subscribers) == 0 {
		q.stopListener <- struct{}{}
	}
	q.subscribeMtx.Unlock()
	close(ch)
}

func (q *Queue) startEventListener() error {
	channel := q.Table + "_events"
	notify, err := q.listener().listen(channel)
	if err != nil {
		return err
	}
	go func() {
	loop:
		for {
			select {
			case n := <-notify:
				if n.err != nil {
					q.listener().unlisten(channel, notify)
					return
				}
				idName := strings.SplitN(n.extra, ":", 2)
				id, err := strconv.ParseInt(idName[0], 10, 64)
				if err != nil {
					fmt.Println("queue: could not parse event id:", err)
					continue loop
				}
				go q.publishEvent(idName[1], id)
			case <-q.stopListener:
				q.listener().unlisten(channel, notify)
				return
			}
		}
	}()
	return nil
}

func (q *Queue) publishEvent(name string, id int64) {
	event, err := q.getEvent(id)
	if err != nil {
		fmt.Printf("queue: error getting event with id %d: %s\n", id, err)
		return
	}
	q.subscribeMtx.RLock()
	defer q.subscribeMtx.RUnlock()
	if s, ok := q.subscribers[name]; ok {
		for ch := range s {
			ch <- event
		}
	}
}

func (q *Queue) getEvent(id int64) (*Event, error) {
	var data []byte
	event := &Event{Job: &Job{Data: data}}
	query := fmt.Sprintf(`
	  SELECT
	    %[1]s_events.id,
	    %[1]s_events.state,
	    %[1]s_events.created_at,
	    %[1]s.id,
	    %[1]s.q_name,
	    %[1]s.data,
	    %[1]s.error_message,
	    %[1]s.attempts,
	    %[1]s.max_attempts,
	    %[1]s.created_at
	  FROM %[1]s_events
	  INNER JOIN %[1]s ON %[1]s_events.job_id = %[1]s.id
	  WHERE %[1]s_events.id = $1`, q.Table)
	return event, q.DB.QueryRow(query, id).Scan(
		&event.ID,
		&event.State,
		&event.CreatedAt,
		&event.Job.ID,
		&event.Job.Name,
		&event.Job.Data,
		&event.Job.ErrorMessage,
		&event.Job.Attempts,
		&event.Job.MaxAttempts,
		&event.Job.CreatedAt,
	)
}
