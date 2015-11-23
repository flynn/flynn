package main

import (
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/host/types"
)

// JobState is a job's in-memory state
type JobState string

const (
	// JobStateStarting is a job's state when it has started in the cluster
	// (i.e. a host.StatusStarting event has been received)
	JobStateStarting JobState = "starting"

	// JobStateRunning is a job's state when it is running in the cluster
	// (i.e. a host.StatusRunning event has been received)
	JobStateRunning JobState = "running"

	// JobStateStopping is a job's state when a request has been made to
	// stop the job in the cluster
	JobStateStopping JobState = "stopping"

	// JobStateStopped is a job's state when it has stopped in the cluster
	// (i.e. either a host.StatusDone, host.StatusCrashed or
	// host.StatusFailed event has been received)
	JobStateStopped JobState = "stopped"

	// JobStateScheduled is a job's state when it is scheduled to start in
	// the future (because it's in the process of being restarted)
	JobStateScheduled JobState = "scheduled"

	// JobStateNew is a job's state when it has been created in-memory (in
	// response to a formation change) and is in the process of being
	// started in the cluster
	JobStateNew JobState = "new"
)

// Job is an in-memory representation of a cluster job
type Job struct {
	// InternalID is used to track jobs in-memory and is added to the
	// cluster job's metadata (with key "flynn-controller.scheduler_id").
	//
	// It is distinct from the cluster job's ID due to the fact that a
	// cluster job only has an ID once a host has been picked to run the
	// job on, and we need to track it before that happens.
	InternalID string

	Type      string
	AppID     string
	ReleaseID string

	// HostID is the ID of the host the job has been placed on, and is set
	// when a StartJob goroutine makes a placement request to the scheduler
	// loop
	HostID string

	// JobID is the ID of the cluster job that this in-memory job
	// represents, and is set either by a StartJob goroutine when it makes
	// a placement request to the scheduler loop, or when an event is
	// received for a yet unknown job (e.g. one started by the controller)
	JobID string

	// Formation is the formation this job belongs to
	Formation *Formation

	// restarts is the number of times this job has been restarted and is
	// used to calculate the amount of time to wait before restarting the
	// job again when it stops (see scheduler.restartJob)
	restarts uint

	// restartTimer is a timer set when scheduling a job to start in the
	// future
	restartTimer *time.Timer

	// startedAt is the time the job started in the cluster, assigned
	// whenever a host event is received for the job, and is used to sort
	// jobs when deciding which job to stop when a formation is scaled down
	startedAt time.Time

	// state is the job's current in-memory state and should only be
	// referenced from within the main scheduler loop
	state JobState

	// metadata is the cluster job's metadata, assigned whenever a host
	// event is received for the job, and is used when persisting the job
	// to the controller
	metadata map[string]string
}

// needsVolume indicates whether a volume should be provisioned in the cluster
// for the job, determined from the corresponding process type in the release
func (j *Job) needsVolume() bool {
	return j.Formation.Release.Processes[j.Type].Data
}

// HasTypeFromRelease indicates whether the job has a type which is present
// in the release
func (j *Job) HasTypeFromRelease() bool {
	for typ := range j.Formation.Release.Processes {
		if j.Type == typ {
			return true
		}
	}
	return false
}

func (j *Job) IsStopped() bool {
	return j.state == JobStateStopping || j.state == JobStateStopped
}

func (j *Job) IsRunning() bool {
	return j.state == JobStateStarting || j.state == JobStateRunning
}

func (j *Job) IsSchedulable() bool {
	return j.Formation != nil && j.Type != ""
}

func (j *Job) IsInFormation(key utils.FormationKey) bool {
	return !j.IsStopped() && j.Formation != nil && j.Formation.key() == key && j.HasTypeFromRelease()
}

type Jobs map[string]*Job

func (j Jobs) WithFormationAndType(f *Formation, typ string) []*Job {
	jobs := make([]*Job, 0, len(j))
	for _, job := range j {
		if job.Formation == f && job.Type == typ {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func (j Jobs) GetHostJobCounts(key utils.FormationKey, typ string) map[string]int {
	counts := make(map[string]int)
	for _, job := range j {
		if job.IsInFormation(key) && job.Type == typ && job.state != JobStateScheduled {
			counts[job.HostID]++
		}
	}
	return counts
}

func (js Jobs) GetProcesses(key utils.FormationKey) Processes {
	procs := make(Processes)
	for _, j := range js {
		if j.IsInFormation(key) {
			procs[j.Type]++
		}
	}
	return procs
}

func (js Jobs) Add(j *Job) {
	js[j.InternalID] = j
}

// TODO refactor `state` to JobStatus type and consolidate statuses across scheduler/controller/host
func controllerJobFromSchedulerJob(job *Job, state string) *ct.Job {
	return &ct.Job{
		ID:        job.JobID,
		AppID:     job.AppID,
		ReleaseID: job.ReleaseID,
		Type:      job.Type,
		State:     state,
		Meta:      utils.JobMetaFromMetadata(job.metadata),
	}
}

func jobState(status host.JobStatus) string {
	switch status {
	case host.StatusStarting:
		return "starting"
	case host.StatusRunning:
		return "up"
	case host.StatusDone:
		return "down"
	case host.StatusCrashed:
		return "crashed"
	case host.StatusFailed:
		return "failed"
	default:
		return ""
	}
}
