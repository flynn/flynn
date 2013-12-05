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
	Error       error
}

type AttachReq struct {
	JobID  string
	Flags  AttachFlag
	Height int
	Width  int
}

type AttachFlag uint8

const (
	AttachFlagStdout AttachFlag = 1 << iota
	AttachFlagStderr
	AttachFlagStdin
	AttachFlagLogs
	AttachFlagStream
)

type JobStatus uint8

func (s JobStatus) String() string {
	return map[JobStatus]string{
		StatusStarting: "starting",
		StatusRunning:  "running",
		StatusDone:     "done",
		StatusCrashed:  "crashed",
		StatusFailed:   "failed",
	}[s]
}

const (
	StatusStarting JobStatus = iota
	StatusRunning
	StatusDone
	StatusCrashed
	StatusFailed
)

const (
	AttachSuccess byte = iota
	AttachWaiting
	AttachError
)
