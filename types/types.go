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

const (
	StatusStarting JobStatus = iota
	StatusRunning
	StatusDone
	StatusCrashed
)

const (
	AttachSuccess byte = iota
	AttachWaiting
	AttachError
)
