package lorne

import (
	"time"

	"github.com/flynn/sampi/types"
)

type Event struct {
	Event string
	JobID string
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
