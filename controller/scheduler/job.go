package main

import (
	"sort"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
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
	ID string `json:"id"`

	Type      string `json:"type"`
	AppID     string `json:"app_id"`
	ReleaseID string `json:"release_id"`

	Args []string `json:"args,omitempty"`

	// HostID is the ID of the host the job has been placed on, and is set
	// when a StartJob goroutine makes a placement request to the scheduler
	// loop
	HostID string `json:"host_id"`

	// JobID is the ID of the cluster job that this in-memory job
	// represents, and is set either by a StartJob goroutine when it makes
	// a placement request to the scheduler loop, or when an event is
	// received for a yet unknown job (e.g. one started by the controller)
	JobID string `json:"job_id"`

	// Formation is the formation this job belongs to
	Formation *Formation `json:"-"`

	// Restarts is the number of times this job has been restarted and is
	// used to calculate the amount of time to wait before restarting the
	// job again when it stops (see scheduler.restartJob)
	Restarts uint `json:"restarts"`

	// restartTimer is a timer set when scheduling a job to start in the
	// future
	restartTimer *time.Timer

	// RunAt is the time we expect this job to be started at if it is the
	// restart of a crashed job.
	RunAt *time.Time `json:"run_at,omitempty"`

	// StartedAt is the time the job started in the cluster, assigned
	// whenever a host event is received for the job, and is used to sort
	// jobs when deciding which job to stop when a formation is scaled down
	StartedAt time.Time `json:"started_at"`

	// State is the job's current in-memory state and should only be
	// referenced from within the main scheduler loop
	State JobState `json:"state"`

	// metadata is the cluster job's metadata, assigned whenever a host
	// event is received for the job, and is used when persisting the job
	// to the controller
	metadata map[string]string

	// exitStatus is the job's exit status once it has stopped running
	exitStatus *int

	// hostError is the error from the host if the job fails to start
	hostError *string

	serviceFirstSeen *time.Time
}

// Tags returns the tags for the job's process type from the formation
func (j *Job) Tags() map[string]string {
	return j.Formation.Tags[j.Type]
}

// TagsMatchHost checks whether all of the job's tags match the corresponding
// host's tags
func (j *Job) TagsMatchHost(host *Host) bool {
	for k, v := range j.Tags() {
		if w, ok := host.Tags[k]; !ok || v != w {
			return false
		}
	}
	return true
}

func (j *Job) Volumes() []ct.VolumeReq {
	proc := j.Formation.Release.Processes[j.Type]
	if len(proc.Volumes) > 0 {
		return proc.Volumes
	} else if proc.DeprecatedData {
		return []ct.VolumeReq{{Path: "/data"}}
	}
	return nil
}

func (j *Job) Service() string {
	if j.Formation == nil {
		return ""
	}
	return j.Formation.Release.Processes[j.Type].Service
}

func (j *Job) IsRunning() bool {
	return j.State == JobStateStarting || j.State == JobStateRunning
}

func (j *Job) IsInFormation(key utils.FormationKey) bool {
	return j.State != JobStateStopped && j.State != JobStateStopping && j.Formation != nil && j.Formation.key() == key
}

func (j *Job) IsInApp(appID string) bool {
	return j.Formation != nil && j.Formation.key().AppID == appID
}

func (j *Job) ControllerJob() *ct.Job {
	job := &ct.Job{
		ID:        j.JobID,
		UUID:      j.ID,
		HostID:    j.HostID,
		AppID:     j.AppID,
		ReleaseID: j.ReleaseID,
		Type:      j.Type,
		Meta:      utils.JobMetaFromMetadata(j.metadata),
		HostError: j.hostError,
		RunAt:     j.RunAt,
		Args:      j.Args,
	}

	switch j.State {
	case JobStatePending:
		job.State = ct.JobStatePending
	case JobStateStarting:
		job.State = ct.JobStateStarting
	case JobStateRunning:
		job.State = ct.JobStateUp
	case JobStateStopping:
		job.State = ct.JobStateStopping
	case JobStateStopped:
		job.State = ct.JobStateDown
	}

	if j.exitStatus != nil {
		job.ExitStatus = typeconv.Int32Ptr(int32(*j.exitStatus))
	}
	if j.Restarts > 0 {
		job.Restarts = typeconv.Int32Ptr(int32(j.Restarts))
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

// sortJobs sorts Jobs in reverse chronological order based on their StartedAt time
type sortJobs []*Job

func (s sortJobs) Len() int           { return len(s) }
func (s sortJobs) Less(i, j int) bool { return s[i].StartedAt.Sub(s[j].StartedAt) > 0 }
func (s sortJobs) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s sortJobs) Sort()              { sort.Sort(s) }
func (s sortJobs) SortReverse()       { sort.Sort(sort.Reverse(s)) }

func (j Jobs) GetHostJobCounts(key utils.FormationKey, typ string) map[string]int {
	counts := make(map[string]int)
	for _, job := range j {
		if job.IsInFormation(key) && job.Type == typ && job.HostID != "" {
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

func (js Jobs) Add(j *Job) *Job {
	js[j.ID] = j
	return j
}
