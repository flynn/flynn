package main

import (
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/host/types"
)

type JobRequestType string
type JobState string

const (
	JobRequestTypeUp   JobRequestType = "up"
	JobRequestTypeDown JobRequestType = "down"
	JobStateUp                        = "running"
	JobStateStopped                   = "stopped"
	JobStateCrashed                   = "crashed"
	JobStateRequesting                = "requesting"
	JobStateNew                       = "new"
)

type JobRequest struct {
	*Job
	RequestType JobRequestType
	attempts    uint
}

func NewJobRequest(f *Formation, requestType JobRequestType, typ, hostID, jobID string) *JobRequest {
	return &JobRequest{
		Job:         NewJob(f, f.App.ID, f.Release.ID, typ, hostID, jobID, JobStateRequesting),
		RequestType: requestType,
	}
}

func (r *JobRequest) needsVolume() bool {
	return r.Job.Formation.Release.Processes[r.Type].Data
}

func (r *JobRequest) Clone() *JobRequest {
	return &JobRequest{
		Job:         r.Job.Clone(),
		RequestType: r.RequestType,
		attempts:    r.attempts,
	}
}

type Job struct {
	Type      string
	AppID     string
	ReleaseID string
	HostID    string
	JobID     string

	Formation *Formation

	restarts  uint
	startedAt time.Time
	state     JobState
}

func NewJob(f *Formation, appID, releaseID, typ, hostID, id string, state JobState) *Job {
	return &Job{
		Type:      typ,
		AppID:     appID,
		ReleaseID: releaseID,
		HostID:    hostID,
		JobID:     id,
		Formation: f,
		startedAt: time.Now(),
		state:     state,
	}
}

func (j *Job) Clone() *Job {
	// Shallow copy
	cloned := *j
	return &cloned
}

func (j *Job) IsScheduled() bool {
	return j.state != JobStateStopped
}

type Jobs map[string]*Job

func (js Jobs) GetFormationJobs(key utils.FormationKey, typ string) []*Job {
	formTypeJobs := make([]*Job, 0, len(js))
	for _, j := range js {
		if j.IsScheduled() && j.Formation != nil && j.Formation.key() == key && j.Type == typ {
			formTypeJobs = append(formTypeJobs, j)
		}
	}
	return formTypeJobs
}

func (js Jobs) GetHostJobCounts(key utils.FormationKey, typ string) map[string]int {
	counts := make(map[string]int)

	for _, j := range js {
		if j.IsScheduled() && j.Formation != nil && j.Formation.key() == key && j.Type == typ {
			counts[j.HostID]++
		}
	}
	return counts
}

func (js Jobs) GetProcesses(key utils.FormationKey) Processes {
	procs := make(Processes)
	for _, j := range js {
		if j.IsScheduled() && j.Formation != nil && j.Formation.key() == key {
			procs[j.Type]++
		}
	}
	return procs
}

// TODO refactor `state` to JobStatus type and consolidate statuses across scheduler/controller/host
func controllerJobFromSchedulerJob(job *Job, state string, metadata map[string]string) *ct.Job {
	return &ct.Job{
		ID:        job.JobID,
		AppID:     job.AppID,
		ReleaseID: job.ReleaseID,
		Type:      job.Type,
		State:     state,
		Meta:      metadata,
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
