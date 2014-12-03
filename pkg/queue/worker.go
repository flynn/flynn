package queue

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/pkg/random"
)

type Worker struct {
	q           *Queue
	name        string
	handler     Handler
	mtx         sync.RWMutex
	stopped     bool
	stop        chan struct{}
	done        chan struct{}
	concurrency int
}

type Job struct {
	ID           int64
	Name         string
	CreatedAt    *time.Time
	Data         []byte
	Attempts     int
	MaxAttempts  int
	ErrorMessage string
	RunAt        *time.Time
}

func newWorker(q *Queue, name string, concurrency int, h Handler) *Worker {
	return &Worker{
		q:           q,
		name:        name,
		handler:     h,
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
		concurrency: concurrency,
	}
}

func (w *Worker) Start() error {
	defer func() {
		w.mtx.Lock()
		w.stopped = true
		w.mtx.Unlock()
		close(w.done)
	}()

	// pool controls how many jobs are worked concurrently
	pool := make(chan struct{}, w.concurrency)
	for i := 0; i < w.concurrency; i++ {
		pool <- struct{}{}
	}
	wait := func() {
		for i := 0; i < w.concurrency; i++ {
			<-pool
		}
	}

	for {
		select {
		case <-w.stop:
			wait()
			return nil
		case <-pool:
			job, err := w.lockJob()
			if err != nil {
				pool <- struct{}{}
				wait()
				if err == errIsStopped {
					return nil
				}
				return err
			}
			go func() {
				w.work(job)
				pool <- struct{}{}
			}()
		}
	}
}

func (w *Worker) Stop() {
	if w.isStopped() {
		return
	}
	close(w.stop)
	<-w.done
}

func (w *Worker) isStopped() bool {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.stopped
}

func (w *Worker) work(job *Job) {
	w.setState(job, JobStateRunning)
	// wrap the handler in a function so we can recover from panics
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		err = w.handler(job)
	}()
	if err != nil {
		job.ErrorMessage = err.Error()
		job.Attempts++

		var runAt time.Time
		if e, ok := err.(RetryAfterError); ok {
			runAt = time.Now().Add(e.delay)
		} else {
			runAt = time.Now().Add(retryDelay(job.Attempts))
		}
		job.RunAt = &runAt

		w.unlock(job)
		if job.Attempts == job.MaxAttempts {
			w.setState(job, JobStateFailed)
		} else {
			w.setState(job, JobStateError)
		}
		return
	}
	w.remove(job)
	w.setState(job, JobStateDone)
}

type RetryAfterError struct {
	delay time.Duration
}

func (e RetryAfterError) Error() string {
	return fmt.Sprintf("retry after error: %s", e.delay)
}

// formula from Sidekiq (originally from delayed_job)
func retryDelay(count int) time.Duration {
	return time.Duration(math.Pow(float64(count), 4)+15+float64(random.Math.Intn(30)*(count+1))) * time.Second
}

var errIsStopped = errors.New("queue: worker is stopped")

func (w *Worker) lockJob() (*Job, error) {
	job := &Job{}

	// get the wait channel before trying to lock a job to avoid missing a new job
	// just after a failed lock attempt
	wait := w.q.Wait(w.name)

	for {
		err := w.q.DB.QueryRow("SELECT id, data, attempts, max_attempts FROM lock_head($1)", w.name).Scan(&job.ID, &job.Data, &job.Attempts, &job.MaxAttempts)
		if err == sql.ErrNoRows {
			select {
			case <-w.stop:
				return nil, errIsStopped
			case err = <-wait:
				if err != nil {
					return nil, err
				}
			// timeout so we can potentially lock a job whose run_at is now in the past.
			case <-time.After(5 * time.Second):
			}
			continue
		} else if err != nil {
			return nil, err
		}
		return job, nil
	}
}

func (w *Worker) setState(job *Job, state JobState) {
	w.q.DB.Exec(fmt.Sprintf("INSERT INTO %s_events (job_id, q_name, attempt, error_message, state) VALUES ($1, $2, $3, $4, $5)", w.q.Table), job.ID, w.name, job.Attempts, job.ErrorMessage, state)
}

func (w *Worker) unlock(job *Job) {
	w.q.DB.Exec(fmt.Sprintf("UPDATE %s set error_message = $1, attempts = $2, locked_at = null, run_at = $3 where id = $4", w.q.Table), job.ErrorMessage, job.Attempts, job.RunAt, job.ID)
}

func (w *Worker) remove(job *Job) {
	w.q.DB.Exec(fmt.Sprintf("DELETE FROM %s where id = $1", w.q.Table), job.ID)
}
