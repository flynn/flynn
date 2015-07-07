package main

import (
	"sync"
	"time"
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
		Job:         NewJob(f, typ, hostID, jobID),
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

func NewJob(f *Formation, typ, hostID, id string) *Job {
	return &Job{
		Type:      typ,
		AppID:     f.App.ID,
		ReleaseID: f.Release.ID,
		HostID:    hostID,
		JobID:     id,
		Formation: f,
	}
}
