package que

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
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

// Test the race condition in LockJob
func TestLockJobAdvisoryRace(t *testing.T) {
	c := openTestClientMaxConns(t, 2)
	defer truncateAndClose(c.pool)

	// *pgx.ConnPool doesn't support pools of only one connection.  Make sure
	// the other one is busy so we know which backend will be used by LockJob
	// below.
	unusedConn, err := c.pool.Acquire()
	if err != nil {
		t.Fatal(err)
	}

	// We use two jobs: the first one is concurrently deleted, and the second
	// one is returned by LockJob after recovering from the race condition.
	for i := 0; i < 2; i++ {
		if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
			t.Fatal(err)
		}
	}

	// helper functions
	newConn := func() *pgx.Conn {
		conn, err := pgx.Connect(testConnConfig)
		if err != nil {
			panic(err)
		}
		return conn
	}
	getBackendID := func(conn *pgx.Conn) int32 {
		var backendID int32
		err := conn.QueryRow(`
			SELECT backendid
			FROM pg_stat_get_backend_idset() psgb(backendid)
			WHERE pg_stat_get_backend_pid(psgb.backendid) = pg_backend_pid()
		`).Scan(&backendID)
		if err != nil {
			panic(err)
		}
		return backendID
	}
	waitUntilBackendIsWaiting := func(backendID int32, name string) {
		conn := newConn()
		i := 0
		for {
			var waiting bool
			err := conn.QueryRow(`SELECT pg_stat_get_backend_waiting($1)`, backendID).Scan(&waiting)
			if err != nil {
				panic(err)
			}

			if waiting {
				break
			} else {
				i++
				if i >= 10000/50 {
					panic(fmt.Sprintf("timed out while waiting for %s", name))
				}

				time.Sleep(50 * time.Millisecond)
			}
		}

	}

	// Reproducing the race condition is a bit tricky.  The idea is to form a
	// lock queue on the relation that looks like this:
	//
	//   AccessExclusive <- AccessShare  <- AccessExclusive ( <- AccessShare )
	//
	// where the leftmost AccessShare lock is the one implicitly taken by the
	// sqlLockJob query.  Once we release the leftmost AccessExclusive lock
	// without releasing the rightmost one, the session holding the rightmost
	// AccessExclusiveLock can run the necessary DELETE before the sqlCheckJob
	// query runs (since it'll be blocked behind the rightmost AccessExclusive
	// Lock).
	//
	deletedJobIDChan := make(chan int64, 1)
	lockJobBackendIDChan := make(chan int32)
	secondAccessExclusiveBackendIDChan := make(chan int32)

	go func() {
		conn := newConn()
		defer conn.Close()

		tx, err := conn.Begin()
		if err != nil {
			panic(err)
		}
		_, err = tx.Exec(`LOCK TABLE que_jobs IN ACCESS EXCLUSIVE MODE`)
		if err != nil {
			panic(err)
		}

		// first wait for LockJob to appear behind us
		backendID := <-lockJobBackendIDChan
		waitUntilBackendIsWaiting(backendID, "LockJob")

		// then for the AccessExclusive lock to appear behind that one
		backendID = <-secondAccessExclusiveBackendIDChan
		waitUntilBackendIsWaiting(backendID, "second access exclusive lock")

		err = tx.Rollback()
		if err != nil {
			panic(err)
		}
	}()

	go func() {
		conn := newConn()
		defer conn.Close()

		// synchronization point
		secondAccessExclusiveBackendIDChan <- getBackendID(conn)

		tx, err := conn.Begin()
		if err != nil {
			panic(err)
		}
		_, err = tx.Exec(`LOCK TABLE que_jobs IN ACCESS EXCLUSIVE MODE`)
		if err != nil {
			panic(err)
		}

		// Fake a concurrent transaction grabbing the job
		var jid int64
		err = tx.QueryRow(`
			DELETE FROM que_jobs
			WHERE job_id =
				(SELECT min(job_id)
				 FROM que_jobs)
			RETURNING job_id
		`).Scan(&jid)
		if err != nil {
			panic(err)
		}

		deletedJobIDChan <- jid

		err = tx.Commit()
		if err != nil {
			panic(err)
		}
	}()

	conn, err := c.pool.Acquire()
	if err != nil {
		panic(err)
	}
	ourBackendID := getBackendID(conn)
	c.pool.Release(conn)

	// synchronization point
	lockJobBackendIDChan <- ourBackendID

	// release the unused connection once the locked job has acquired the
	// other connection so it can be used by the job's LockedUntil goroutine.
	go func() {
		for {
			if c.pool.Stat().AvailableConnections == 0 {
				c.pool.Release(unusedConn)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	job, err := c.LockJob("")
	if err != nil {
		panic(err)
	}
	defer job.Done()

	deletedJobID := <-deletedJobIDChan

	t.Logf("Got id %d", job.ID)
	t.Logf("Concurrently deleted id %d", deletedJobID)

	if deletedJobID >= job.ID {
		t.Fatalf("deleted job id %d must be smaller than job.ID %d", deletedJobID, job.ID)
	}
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

func TestJobDisconnect(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	j1, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	defer j1.Done()

	// kill the job's connection, which will drop the advisory lock
	conn := j1.Conn()
	if conn == nil {
		t.Fatal("job has no connection")
	}
	var ok bool
	if err := c.pool.QueryRow("SELECT pg_terminate_backend($1)", conn.Pid).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want pg_terminate_backend=true, got false")
	}

	// make sure the job is still locked
	j2, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j2 != nil {
		defer j2.Done()
		t.Fatalf("wanted no job, got %+v", j2)
	}

	// finish the job, and check it can be locked again
	j1.Done()
	j3, err := c.LockJob("")
	if err != nil {
		t.Fatal(err)
	}
	if j3 == nil {
		t.Fatal("job was not found")
	}
	j3.Done()
}
