package main

import (
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
)

type JobRequestType string

const (
	JobRequestTypeUp   JobRequestType = "up"
	JobRequestTypeDown JobRequestType = "down"
)

type JobRequest struct {
	*Job
	RequestType JobRequestType
}

func NewJobRequest(f *Formation, requestType JobRequestType, typ, hostID, jobID string) *JobRequest {
	return &JobRequest{
		Job:         NewJob(f, f.App.ID, f.Release.ID, typ, hostID, jobID, time.Now()),
		RequestType: requestType,
	}
}

func (r *JobRequest) needsVolume() bool {
	return r.Job.Formation.Release.Processes[r.Type].Data
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
}

func NewJob(f *Formation, appID, releaseID, typ, hostID, id string, startedAt time.Time) *Job {
	return &Job{
		Type:      typ,
		AppID:     appID,
		ReleaseID: releaseID,
		HostID:    hostID,
		JobID:     id,
		Formation: f,
		startedAt: startedAt,
	}
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
