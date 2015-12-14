package main

import (
	"sort"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/typeconv"
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

	// JobStatePending is a job's state when it is scheduled to start in
	// the future, either because it is a new job being started due to a
	// formation change, or is scheduled to replace a crashed job after a
	// backoff period
	JobStatePending JobState = "pending"
)

// Job is an in-memory representation of a cluster job
type Job struct {
	// ID is used to track jobs in-memory and is the UUID part of the
	// cluster job's ID.
	//
	// We only use the UUID part due to the fact that a cluster job only
	// has a HostID once a host has been picked to run the job on, and we
	// need to track it before that happens.
	ID string

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

	// runAt is the time we expect this job to be started at if it is the
	// restart of a crashed job.
	runAt *time.Time

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

	// exitStatus is the job's exit status once it has stopped running
	exitStatus *int

	// hostError is the error from the host if the job fails to start
	hostError *string
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

func (j *Job) ControllerJob() *ct.Job {
	job := &ct.Job{
		ID:        cluster.GenerateJobID(j.HostID, j.ID),
		AppID:     j.AppID,
		ReleaseID: j.ReleaseID,
		Type:      j.Type,
		Meta:      utils.JobMetaFromMetadata(j.metadata),
		HostError: j.hostError,
		RunAt:     j.runAt,
	}

	switch j.state {
	case JobStatePending:
		job.State = ct.JobStatePending
	case JobStateStarting:
		job.State = ct.JobStateStarting
	case JobStateRunning:
		job.State = ct.JobStateUp
	case JobStateStopped:
		job.State = ct.JobStateDown
	}

	if j.exitStatus != nil {
		job.ExitStatus = typeconv.Int32Ptr(int32(*j.exitStatus))
	}
	if j.restarts > 0 {
		job.Restarts = typeconv.Int32Ptr(int32(j.restarts))
	}

	return job
}

type Jobs map[string]*Job

// WithFormationAndType returns a list of jobs which belong to the given
// formation and have the given type, ordered with the most recently started
// job first
func (j Jobs) WithFormationAndType(f *Formation, typ string) sortJobs {
	jobs := make(sortJobs, 0, len(j))
	for _, job := range j {
		if job.Formation == f && job.Type == typ {
			jobs = append(jobs, job)
		}
	}
	jobs.Sort()
	return jobs
}

// sortJobs sorts Jobs in reverse chronological order based on their startedAt time
type sortJobs []*Job

func (s sortJobs) Len() int           { return len(s) }
func (s sortJobs) Less(i, j int) bool { return s[i].startedAt.Sub(s[j].startedAt) > 0 }
func (s sortJobs) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s sortJobs) Sort()              { sort.Sort(s) }

func (j Jobs) GetHostJobCounts(key utils.FormationKey, typ string) map[string]int {
	counts := make(map[string]int)
	for _, job := range j {
		if job.IsInFormation(key) && job.Type == typ && job.restartTimer == nil {
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
	js[j.ID] = j
}
