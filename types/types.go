package lorne

import (
	"syscall"
	"time"

	"github.com/flynn/sampi/types"
)

type JobSignal struct {
	JobID  string
	Signal syscall.Signal
}

type Job struct {
	Job *sampi.Job

	Status    JobStatus
	StartedAt time.Time
	EndedAt   time.Time
	// TODO: exit code?
}

type JobStatus uint8

const (
	StatusStarting JobStatus = iota
	StatusRunning
	StatusDone
	StatusCrashed
)
