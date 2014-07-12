package host

import (
	"time"
)

type Job struct {
	ID string

	Metadata map[string]string

	Artifact  Artifact
	Resources JobResources

	Config ContainerConfig
}

type JobResources struct {
	Memory int // in KiB
}

type ContainerConfig struct {
	TTY        bool
	Stdin      bool
	Data       bool
	Entrypoint []string
	Cmd        []string
	Env        map[string]string
	Mounts     []Mount
	Ports      []Port
	WorkingDir string
	Uid        int
}

type Port struct {
	Port     int
	Proto    string
	RangeEnd int
}

type Mount struct {
	Location  string
	Target    string
	Writeable bool
}

type Artifact struct {
	URI  string
	Type string
}

type Host struct {
	ID string

	Jobs     []*Job
	Metadata map[string]string
}

type AddJobsReq struct {
	// map of host id -> new jobs
	HostJobs map[string][]*Job
}

type AddJobsRes struct {
	// The state of the cluster after the operation
	State map[string]Host
}

type Event struct {
	Event string
	JobID string
	Job   *ActiveJob
}

type HostEvent struct {
	Event  string
	HostID string
}

type ActiveJob struct {
	Job         *Job
	ContainerID string
	InternalIP  string
	Status      JobStatus
	StartedAt   time.Time
	EndedAt     time.Time
	ExitStatus  int
	Error       *string
	ManifestID  string
}

type AttachReq struct {
	JobID  string
	Flags  AttachFlag
	Height uint16
	Width  uint16
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
	AttachData
	AttachSignal
	AttachExit
	AttachResize
)
