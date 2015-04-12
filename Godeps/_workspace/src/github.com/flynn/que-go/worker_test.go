package que

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

func init() {
	log.SetOutput(ioutil.Discard)
}

func TestWorkerWorkOne(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	success := false
	wm := WorkMap{
		"MyJob": func(j *Job) error {
			success = true
			return nil
		},
	}
	w := NewWorker(c, wm)

	didWork := w.WorkOne()
	if didWork {
		t.Errorf("want didWork=false when no job was queued")
	}

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	didWork = w.WorkOne()
	if !didWork {
		t.Errorf("want didWork=true")
	}
	if !success {
		t.Errorf("want success=true")
	}
}

func TestWorkerShutdown(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	w := NewWorker(c, WorkMap{})
	done := make(chan struct{})
	go func() {
		w.Work()
		close(done)
	}()
	w.Shutdown()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker to shutdown")
	}

	if !w.done {
		t.Errorf("want w.done=true")
	}
}

func BenchmarkWorker(b *testing.B) {
	c := openTestClient(b)
	log.SetOutput(ioutil.Discard)
	defer func() {
		log.SetOutput(os.Stdout)
	}()
	defer truncateAndClose(c.pool)

	w := NewWorker(c, WorkMap{"Nil": nilWorker})

	for i := 0; i < b.N; i++ {
		if err := c.Enqueue(&Job{Type: "Nil"}); err != nil {
			log.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.WorkOne()
	}
}

func nilWorker(j *Job) error {
	return nil
}

func TestWorkerWorkReturnsError(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	called := 0
	wm := WorkMap{
		"MyJob": func(j *Job) error {
			called++
			return fmt.Errorf("the error msg")
		},
	}
	w := NewWorker(c, wm)

	didWork := w.WorkOne()
	if didWork {
		t.Errorf("want didWork=false when no job was queued")
	}

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	didWork = w.WorkOne()
	if !didWork {
		t.Errorf("want didWork=true")
	}
	if called != 1 {
		t.Errorf("want called=1 was: %d", called)
	}

	tx, err := c.pool.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	j, err := findOneJob(tx)
	if err != nil {
		t.Fatal(err)
	}
	if j.ErrorCount != 1 {
		t.Errorf("want ErrorCount=1 was %d", j.ErrorCount)
	}
	if !j.LastError.Valid {
		t.Errorf("want LastError IS NOT NULL")
	}
	if j.LastError.String != "the error msg" {
		t.Errorf("want LastError=\"the error msg\" was: %q", j.LastError.String)
	}
}

func TestWorkerWorkRescuesPanic(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	called := 0
	wm := WorkMap{
		"MyJob": func(j *Job) error {
			called++
			panic("the panic msg")
			return nil
		},
	}
	w := NewWorker(c, wm)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	w.WorkOne()
	if called != 1 {
		t.Errorf("want called=1 was: %d", called)
	}

	tx, err := c.pool.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	j, err := findOneJob(tx)
	if err != nil {
		t.Fatal(err)
	}
	if j.ErrorCount != 1 {
		t.Errorf("want ErrorCount=1 was %d", j.ErrorCount)
	}
	if !j.LastError.Valid {
		t.Errorf("want LastError IS NOT NULL")
	}
	if !strings.Contains(j.LastError.String, "the panic msg\n") {
		t.Errorf("want LastError contains \"the panic msg\\n\" was: %q", j.LastError.String)
	}
	// basic check if a stacktrace is there - not the stacktrace format itself
	if !strings.Contains(j.LastError.String, "worker.go:") {
		t.Errorf("want LastError contains \"worker.go:\" was: %q", j.LastError.String)
	}
	if !strings.Contains(j.LastError.String, "worker_test.go:") {
		t.Errorf("want LastError contains \"worker_test.go:\" was: %q", j.LastError.String)
	}
}

func TestWorkerWorkOneTypeNotInMap(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	currentConns := c.pool.Stat().CurrentConnections
	availConns := c.pool.Stat().AvailableConnections

	success := false
	wm := WorkMap{}
	w := NewWorker(c, wm)

	didWork := w.WorkOne()
	if didWork {
		t.Errorf("want didWork=false when no job was queued")
	}

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	didWork = w.WorkOne()
	if !didWork {
		t.Errorf("want didWork=true")
	}
	if success {
		t.Errorf("want success=false")
	}

	if currentConns != c.pool.Stat().CurrentConnections-1 {
		t.Errorf("want currentConns equal: before=%d  after=%d", currentConns, c.pool.Stat().CurrentConnections)
	}
	if availConns != c.pool.Stat().AvailableConnections-1 {
		t.Errorf("want availConns equal: before=%d  after=%d", availConns, c.pool.Stat().AvailableConnections)
	}

	tx, err := c.pool.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	j, err := findOneJob(tx)
	if err != nil {
		t.Fatal(err)
	}
	if j.ErrorCount != 1 {
		t.Errorf("want ErrorCount=1 was %d", j.ErrorCount)
	}
	if !j.LastError.Valid {
		t.Fatal("want non-nil LastError")
	}
	if want := "unknown job type: \"MyJob\""; j.LastError.String != want {
		t.Errorf("want LastError=%q, got %q", want, j.LastError.String)
	}

}
