package main

import (
	"errors"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/host/types"
)

type JobState string

const (
	JobStateStarting  JobState = "starting"
	JobStateRunning   JobState = "running"
	JobStateStopping  JobState = "stopping"
	JobStateStopped   JobState = "stopped"
	JobStateCrashed   JobState = "crashed"
	JobStateScheduled JobState = "scheduled"
	JobStateNew       JobState = "new"
)

type JobRequest struct {
	Job      *Job
	attempts uint
}

func NewJobRequest(f *Formation, typ, hostID, internalID string) *JobRequest {
	return &JobRequest{Job: NewJob(f, f.App.ID, f.Release.ID, typ, hostID, internalID)}
}

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
	HostID    string
	JobID     string

	Formation *Formation

	restarts  uint
	startedAt time.Time
	state     JobState
}

func NewJob(f *Formation, appID, releaseID, typ, hostID, internalID string) *Job {
	return &Job{
		InternalID: internalID,
		Type:       typ,
		AppID:      appID,
		ReleaseID:  releaseID,
		HostID:     hostID,
		Formation:  f,
		startedAt:  time.Now(),
		state:      JobStateNew,
	}
}

func (j *Job) needsVolume() bool {
	return j.Formation.Release.Processes[j.Type].Data
}

func (j *Job) IsStopped() bool {
	return j.state == JobStateStopping || j.state == JobStateStopped || j.state == JobStateCrashed
}

func (j *Job) IsRunning() bool {
	return j.state == JobStateStarting || j.state == JobStateRunning
}

func (j *Job) IsSchedulable() bool {
	return j.Formation != nil && j.Type != ""
}

func (j *Job) IsInFormation(key utils.FormationKey) bool {
	return !j.IsStopped() && j.Formation != nil && j.Formation.key() == key
}

type Jobs map[string]*Job

func (js Jobs) GetStoppableJobs(key utils.FormationKey, typ string) []*Job {
	formTypeJobs := make([]*Job, 0, len(js))
	for _, j := range js {
		if j.IsInFormation(key) && j.IsRunning() && j.Type == typ {
			formTypeJobs = append(formTypeJobs, j)
		}
	}
	return formTypeJobs
}

func (js Jobs) GetHostJobCounts(key utils.FormationKey, typ string) map[string]int {
	counts := make(map[string]int)

	for _, j := range js {
		if j.IsInFormation(key) && j.Type == typ && !j.HasState(JobStateScheduled) {
			counts[j.HostID]++
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

func (js Jobs) AddJob(j *Job) {
	js[j.InternalID] = j
}

func (js Jobs) IsJobInState(id string, state JobState) bool {
	j, ok := js[id]
	return ok && j.state == state
}

func (js Jobs) SetState(id string, state JobState) error {
	if j, ok := js[id]; ok {
		j.state = state
		return nil
	}
	return errors.New("job not found")
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
