package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/pkg/testutils"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
	q *Queue
}

var _ = ConcurrentSuite(&S{})

func (s *S) SetUpSuite(c *C) {
	dbname := "queuetest"
	c.Assert(testutils.SetupPostgres(dbname), IsNil)

	dsn := fmt.Sprintf("dbname=%s", dbname)
	db, err := sql.Open("postgres", dsn)
	c.Assert(err, IsNil)

	s.q = New(db, "jobs")
	c.Assert(s.q.SetupDB(), IsNil)
}

type testHelper struct {
	c    *C
	q    *Queue
	w    *Worker
	ch   chan *Event
	name string
}

func newTestHelper(c *C, q *Queue, name string, concurrency int, h Handler) *testHelper {
	ch, err := q.Subscribe(name)
	c.Assert(err, IsNil)
	w := q.NewWorker(name, concurrency, h)
	go w.Start()
	return &testHelper{c, q, w, ch, name}
}

func (t *testHelper) push(v interface{}) {
	_, err := t.q.Push(t.name, t.marshal(v))
	t.c.Assert(err, IsNil)
}

func (t *testHelper) pushWithMaxAttempts(v interface{}, maxAttempts int) *Job {
	job, err := t.q.PushWithMaxAttempts(t.name, t.marshal(v), maxAttempts)
	t.c.Assert(err, IsNil)
	return job
}

func (t *testHelper) marshal(v interface{}) []byte {
	if data, ok := v.([]byte); ok {
		return data
	}
	data, err := json.Marshal(v)
	t.c.Assert(err, IsNil)
	return data
}

func (t *testHelper) waitForEventCondition(condition func(*Event) bool) {
	for {
		select {
		case e := <-t.ch:
			if condition(e) {
				return
			}
		case <-time.After(time.Second):
			t.c.Fatal("timed out waiting for job event")
		}
	}
}

func (t *testHelper) waitForJobFailure() {
	t.waitForEventCondition(func(e *Event) bool {
		return e.State == JobStateFailed
	})
}

func (t *testHelper) checkJobError(id int64, errorMsg string) {
	job, err := t.q.Get(id)
	t.c.Assert(err, IsNil)
	t.c.Assert(job.ErrorMessage, Equals, errorMsg)
}

func (t *testHelper) cleanup() {
	t.q.Unsubscribe(t.name, t.ch)
	done := make(chan struct{})
	go func() {
		t.w.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.c.Fatal("timed out stopping worker")
	}
}

func (s *S) TestWork(c *C) {
	name := "test-work"

	var job *Job
	handler := func(j *Job) error {
		job = j
		return nil
	}

	h := newTestHelper(c, s.q, name, 1, handler)
	defer h.cleanup()

	payload := "a message"
	h.push(payload)

	h.waitForEventCondition(func(e *Event) bool {
		return e.State == JobStateDone
	})

	var actual string
	c.Assert(json.Unmarshal(job.Data, &actual), IsNil)
	c.Assert(actual, Equals, payload)
}

func (s *S) TestWorkMultipleJobs(c *C) {
	name := "test-multiple-jobs"

	var mtx sync.Mutex
	count := make(map[int64]int)
	handler := func(j *Job) error {
		mtx.Lock()
		count[j.ID]++
		mtx.Unlock()
		return nil
	}

	h := newTestHelper(c, s.q, name, 5, handler)
	defer h.cleanup()

	expected := 10
	for i := 0; i < expected; i++ {
		h.push([]byte("job"))
	}

	actual := 0
	h.waitForEventCondition(func(e *Event) bool {
		if e.State == JobStateDone {
			actual++
		}
		return actual >= expected
	})
	c.Assert(len(count), Equals, expected)
}

func (s *S) TestMaxAttempts(c *C) {
	name := "test-max-attempts"

	var mtx sync.Mutex
	count := 0
	handler := func(j *Job) error {
		mtx.Lock()
		count++
		mtx.Unlock()
		return RetryAfterError{0}
	}

	h := newTestHelper(c, s.q, name, 5, handler)
	defer h.cleanup()

	maxAttempts := 20
	h.pushWithMaxAttempts([]byte("max-attempts"), 20)
	h.waitForJobFailure()
	c.Assert(count, Equals, maxAttempts)
}

func (s *S) TestJobError(c *C) {
	name := "test-job-error"
	errorMsg := "ERROR!"

	handler := func(j *Job) error {
		return errors.New(errorMsg)
	}

	h := newTestHelper(c, s.q, name, 1, handler)
	defer h.cleanup()

	job := h.pushWithMaxAttempts([]byte("error"), 1)
	h.waitForJobFailure()
	h.checkJobError(job.ID, errorMsg)
}

func (s *S) TestPanicHandler(c *C) {
	name := "test-panic-handler"

	handler := func(j *Job) error {
		panic("arghh")
	}

	h := newTestHelper(c, s.q, name, 1, handler)
	defer h.cleanup()

	job := h.pushWithMaxAttempts([]byte("panic"), 1)
	h.waitForJobFailure()
	h.checkJobError(job.ID, "panic: arghh")
}
