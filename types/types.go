package host

import (
	"strconv"
	"time"

	"github.com/flynn/go-dockerclient"
)

type Job struct {
	ID string

	// The ID of the container to run
	Container string
	// Job attributes (all host rules must match successfully)
	Attributes map[string]string
	// Resource requirements (decremented from host resources)
	Resources map[string]int
	// Number of TCP ports required by the job
	// TODO: move to Attrs/Resources?
	TCPPorts int

	Config     *docker.Config
	HostConfig *docker.HostConfig
}

type ResourceValue struct {
	Value      int  `json:"value"`
	Overcommit bool `json:"overcommit"`
}

type Host struct {
	ID string

	// Currently running jobs
	Jobs []*Job
	// All rules must match job attributes
	Rules []Rule
	// Currently available resources
	Resources map[string]ResourceValue
	// Host attributes
	Attributes map[string]string
}

func (h *Host) Compatible(job *Job) bool {
	for _, r := range h.Rules {
		if !r.Match(job.Attributes) {
			return false
		}
	}
	return true
}

func (h *Host) Fits(job *Job) bool {
	for k, v := range job.Resources {
		res, ok := h.Resources[k]
		if !ok || !res.Overcommit && res.Value < v {
			return false
		}
	}
	return true
}

func (h *Host) Add(job *Job) bool {
	if !h.Compatible(job) {
		return false
	}
	for k, v := range job.Resources {
		res, ok := h.Resources[k]
		if !ok || !res.Overcommit && res.Value < v {
			return false
		}
		res.Value -= v
		h.Resources[k] = res
	}
	h.Jobs = append(h.Jobs, job)
	return true
}

type AddJobsReq struct {
	// If true, commit all jobs that fit; if false, reject entire request if a single job doesn't fit
	Incremental bool
	// map of host id -> new jobs
	HostJobs map[string][]*Job
}

type AddJobsRes struct {
	// The state of the cluster after the operation
	State map[string]Host
	// If the request was incremental, the jobs that were not scheduled
	RemainingJobs []*Job
	// true if the request was atomic and all jobs were committed
	Success bool
}

type RuleOperator uint8

const (
	OpEq RuleOperator = iota
	OpNotEq
	OpGt
	OpGtEq
	OpLt
	OpLtEq
)

type Rule struct {
	Key   string
	Op    RuleOperator
	Value string
}

func (r *Rule) Match(h map[string]string) bool {
	v, ok := h[r.Key]
	if !ok {
		return r.Op == OpEq && r.Value == "nil"
	}
	switch r.Op {
	case OpEq:
		return r.Value == v
	case OpNotEq:
		return r.Value != v
	}

	// TODO: cache these somewhere
	left, _ := strconv.Atoi(v)
	right, _ := strconv.Atoi(r.Value)
	switch r.Op {
	case OpGt:
		return left > right
	case OpGtEq:
		return left >= right
	case OpLt:
		return left < right
	case OpLtEq:
		return left <= right
	default:
		// invalid op
		return false
	}
}

type Event struct {
	Event string
	JobID string
}

type ActiveJob struct {
	Job *Job

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
