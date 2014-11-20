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

func (j *Job) Dup() *Job {
	job := *j

	dupMap := func(m map[string]string) (res map[string]string) {
		if m != nil {
			res = make(map[string]string, len(m))
		}
		for k, v := range m {
			res[k] = v
		}
		return res
	}
	dupSlice := func(s []string) (res []string) {
		if s != nil {
			res = make([]string, len(s))
		}
		for i, v := range s {
			res[i] = v
		}
		return res
	}
	job.Metadata = dupMap(j.Metadata)
	job.Config.Entrypoint = dupSlice(j.Config.Entrypoint)
	job.Config.Cmd = dupSlice(j.Config.Cmd)
	job.Config.Env = dupMap(j.Config.Env)
	if j.Config.Ports != nil {
		job.Config.Ports = make([]Port, len(j.Config.Ports))
		for i, p := range j.Config.Ports {
			job.Config.Ports[i] = p
		}
	}
	if j.Config.Mounts != nil {
		job.Config.Mounts = make([]Mount, len(j.Config.Mounts))
		for i, m := range j.Config.Mounts {
			job.Config.Mounts[i] = m
		}
	}

	return &job
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
	HostID      string
	ContainerID string
	InternalIP  string
	ForceStop   bool
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
