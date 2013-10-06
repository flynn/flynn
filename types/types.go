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

	ContainerID string
	Status      JobStatus
	StartedAt   time.Time
	EndedAt     time.Time
	ExitCode    int
}

type JobStatus uint8

const (
	StatusStarting JobStatus = iota
	StatusRunning
	StatusDone
	StatusCrashed
)
