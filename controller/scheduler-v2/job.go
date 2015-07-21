package main

import (
	"sync"
	"time"

	ct "github.com/flynn/flynn/controller/types"
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
		Job:         NewJob(f, typ, hostID, jobID, time.Time{}),
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

	restarts  int
	timer     *time.Timer
	timerMtx  sync.Mutex
	startedAt time.Time
}

func NewJob(f *Formation, typ, hostID, id string, startedAt time.Time) *Job {
	return &Job{
		Type:      typ,
		AppID:     f.App.ID,
		ReleaseID: f.Release.ID,
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
