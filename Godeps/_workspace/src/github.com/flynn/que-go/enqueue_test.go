package que

import (
	"testing"
	"time"
)

func TestEnqueueOnlyType(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	j, err := findOneJob(c.pool)
	if err != nil {
		t.Fatal(err)
	}

	// check resulting job
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
}

func TestEnqueueWithPriority(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	want := int16(99)
	if err := c.Enqueue(&Job{Type: "MyJob", Priority: want}); err != nil {
		t.Fatal(err)
	}

	j, err := findOneJob(c.pool)
	if err != nil {
		t.Fatal(err)
	}

	if j.Priority != want {
		t.Errorf("want Priority=%d, got %d", want, j.Priority)
	}
}

func TestEnqueueWithRunAt(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	want := time.Now().Add(2 * time.Minute)
	if err := c.Enqueue(&Job{Type: "MyJob", RunAt: want}); err != nil {
		t.Fatal(err)
	}

	j, err := findOneJob(c.pool)
	if err != nil {
		t.Fatal(err)
	}

	// truncate to the microsecond as postgres driver does
	want = want.Truncate(time.Microsecond)
	if !want.Equal(j.RunAt) {
		t.Errorf("want RunAt=%s, got %s", want, j.RunAt)
	}
}

func TestEnqueueWithArgs(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	want := `{"arg1":0, "arg2":"a string"}`
	if err := c.Enqueue(&Job{Type: "MyJob", Args: []byte(want)}); err != nil {
		t.Fatal(err)
	}

	j, err := findOneJob(c.pool)
	if err != nil {
		t.Fatal(err)
	}

	if got := string(j.Args); got != want {
		t.Errorf("want Args=%s, got %s", want, got)
	}
}

func TestEnqueueWithQueue(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	want := "special-work-queue"
	if err := c.Enqueue(&Job{Type: "MyJob", Queue: want}); err != nil {
		t.Fatal(err)
	}

	j, err := findOneJob(c.pool)
	if err != nil {
		t.Fatal(err)
	}

	if j.Queue != want {
		t.Errorf("want Queue=%q, got %q", want, j.Queue)
	}
}

func TestEnqueueWithEmptyType(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	if err := c.Enqueue(&Job{Type: ""}); err != ErrMissingType {
		t.Fatalf("want ErrMissingType, got %v", err)
	}
}

func TestEnqueueInTx(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(c.pool)

	tx, err := c.pool.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if err = c.EnqueueInTx(&Job{Type: "MyJob"}, tx); err != nil {
		t.Fatal(err)
	}

	j, err := findOneJob(tx)
	if err != nil {
		t.Fatal(err)
	}
	if j == nil {
		t.Fatal("want job, got none")
	}

	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	j, err = findOneJob(c.pool)
	if err != nil {
		t.Fatal(err)
	}
	if j != nil {
		t.Fatalf("wanted job to be rolled back, got %+v", j)
	}
}
