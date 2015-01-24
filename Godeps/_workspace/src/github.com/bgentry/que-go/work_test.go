package que

import (
	"sync"
	"testing"
)

func TestLockJob(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	j, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}

	if j.conn == nil {
		t.Fatal("want non-nil conn on locked Job")
	}
	if j.pool == nil {
		t.Fatal("want non-nil pool on locked Job")
	}
	defer j.Done()

	// check values of returned Job
	if j.ID == 0 {
		t.Errorf("want non-zero ID")
	}
	if want := ""; j.Queue != want {
		t.Errorf("want Queue=%q, got %q", want, j.Queue)
	}
	if want := int16(100); j.Priority != want {
		t.Errorf("want Priority=%d, got %d", want, j.Priority)
	}
	if j.RunAt.IsZero() {
		t.Error("want non-zero RunAt")
	}
	if want := "MyJob"; j.Type != want {
		t.Errorf("want Type=%q, got %q", want, j.Type)
	}
	if want, got := "[]", string(j.Args); got != want {
		t.Errorf("want Args=%s, got %s", want, got)
	}
	if want := int32(0); j.ErrorCount != want {
		t.Errorf("want ErrorCount=%d, got %d", want, j.ErrorCount)
	}
	if j.LastError.Valid {
		t.Errorf("want no LastError, got %v", j.LastError)
	}

	// check for advisory lock
	var count int64
	query := "SELECT count(*) FROM pg_locks WHERE locktype=$1 AND objid=$2::bigint"
	if err = j.pool.QueryRow(query, "advisory", j.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("want 1 advisory lock, got %d", count)
	}

	// make sure conn was checked out of pool
	stat := c.pool.Stat()
	total, available := stat.CurrentConnections, stat.AvailableConnections
	if want := total - 1; available != want {
		t.Errorf("want available=%d, got %d", want, available)
	}

	if err = j.Delete(); err != nil {
		t.Fatal(err)
	}
}

func TestLockJobAlreadyLocked(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	j, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j == nil {
		t.Fatal("wanted job, got none")
	}
	defer j.Done()

	j2, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j2 != nil {
		defer j2.Done()
		t.Fatalf("wanted no job, got %+v", j2)
	}
}

func TestLockJobNoJob(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	j, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j != nil {
		t.Errorf("want no job, got %v", j)
	}
}

func TestLockJobCustomQueue(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob", Queue: "extra_priority"}); err != nil {
		t.Fatal(err)
	}

	j, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j != nil {
		j.Done()
		t.Errorf("expected no job to be found with empty queue name, got %+v", j)
	}

	j, err = c.LockJob("extra_priority")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Done()

	if j == nil {
		t.Fatal("wanted job, got none")
	}

	if err = j.Delete(); err != nil {
		t.Fatal(err)
	}
}

func TestJobConn(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	j, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j == nil {
		t.Fatal("wanted job, got none")
	}
	defer j.Done()

	if conn := j.Conn(); conn != j.conn {
		t.Errorf("want %+v, got %+v", j.conn, conn)
	}
}

func TestJobConnRace(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	j, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j == nil {
		t.Fatal("wanted job, got none")
	}
	defer j.Done()

	var wg sync.WaitGroup
	wg.Add(2)

	// call Conn and Done in different goroutines to make sure they are safe from
	// races.
	go func() {
		_ = j.Conn()
		wg.Done()
	}()
	go func() {
		j.Done()
		wg.Done()
	}()
	wg.Wait()
}

func TestJobDelete(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	j, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j == nil {
		t.Fatal("wanted job, got none")
	}
	defer j.Done()

	if err = j.Delete(); err != nil {
		t.Fatal(err)
	}

	// make sure job was deleted
	j2, err := findOneJob(c.pool)
	if err != nil {
		t.Fatal(err)
	}
	if j2 != nil {
		t.Errorf("job was not deleted: %+v", j2)
	}
}

func TestJobDone(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	j, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j == nil {
		t.Fatal("wanted job, got none")
	}

	j.Done()

	// make sure conn and pool were cleared
	if j.conn != nil {
		t.Errorf("want nil conn, got %+v", j.conn)
	}
	if j.pool != nil {
		t.Errorf("want nil pool, got %+v", j.pool)
	}

	// make sure lock was released
	var count int64
	query := "SELECT count(*) FROM pg_locks WHERE locktype=$1 AND objid=$2::bigint"
	if err = c.pool.QueryRow(query, "advisory", j.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("advisory lock was not released")
	}

	// make sure conn was returned to pool
	stat := c.pool.Stat()
	total, available := stat.CurrentConnections, stat.AvailableConnections
	if total != available {
		t.Errorf("want available=total, got available=%d total=%d", available, total)
	}
}

func TestJobDoneMultiple(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	j, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j == nil {
		t.Fatal("wanted job, got none")
	}

	j.Done()
	// try calling Done() again
	j.Done()
}

func TestJobDeleteFromTx(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	j, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j == nil {
		t.Fatal("wanted job, got none")
	}

	// get the job's database connection
	conn := j.Conn()
	if conn == nil {
		t.Fatal("wanted conn, got nil")
	}

	// start a transaction
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// delete the job
	if err = j.Delete(); err != nil {
		t.Fatal(err)
	}

	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// mark as done
	j.Done()

	// make sure the job is gone
	j2, err := findOneJob(c.pool)
	if err != nil {
		t.Fatal(err)
	}

	if j2 != nil {
		t.Errorf("wanted no job, got %+v", j2)
	}
}

func TestJobDeleteFromTxRollback(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	j1, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j1 == nil {
		t.Fatal("wanted job, got none")
	}

	// get the job's database connection
	conn := j1.Conn()
	if conn == nil {
		t.Fatal("wanted conn, got nil")
	}

	// start a transaction
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// delete the job
	if err = j1.Delete(); err != nil {
		t.Fatal(err)
	}

	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// mark as done
	j1.Done()

	// make sure the job still exists and matches j1
	j2, err := findOneJob(c.pool)
	if err != nil {
		t.Fatal(err)
	}

	if j1.ID != j2.ID {
		t.Errorf("want job %d, got %d", j1.ID, j2.ID)
	}
}

func TestJobError(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	j, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j == nil {
		t.Fatal("wanted job, got none")
	}
	defer j.Done()

	msg := "world\nended"
	if err = j.Error(msg); err != nil {
		t.Fatal(err)
	}
	j.Done()

	// make sure job was not deleted
	j2, err := findOneJob(c.pool)
	if err != nil {
		t.Fatal(err)
	}
	if j2 == nil {
		t.Fatal("job was not found")
	}
	defer j2.Done()

	if !j2.LastError.Valid || j2.LastError.String != msg {
		t.Errorf("want LastError=%q, got %q", msg, j2.LastError.String)
	}
	if j2.ErrorCount != 1 {
		t.Errorf("want ErrorCount=%d, got %d", 1, j2.ErrorCount)
	}

	// make sure lock was released
	var count int64
	query := "SELECT count(*) FROM pg_locks WHERE locktype=$1 AND objid=$2::bigint"
	if err = c.pool.QueryRow(query, "advisory", j.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("advisory lock was not released")
	}

	// make sure conn was returned to pool
	stat := c.pool.Stat()
	total, available := stat.CurrentConnections, stat.AvailableConnections
	if total != available {
		t.Errorf("want available=total, got available=%d total=%d", available, total)
	}
}
