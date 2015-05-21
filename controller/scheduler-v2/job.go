package main

import (
	"sync"
	"time"
)

type jobKey struct {
	hostID, jobID string
}

type JobRequestType string

const (
	JobRequestTypeUp   JobRequestType = "up"
	JobRequestTypeDown JobRequestType = "down"
)

type JobSpec struct {
	JobType   string
	AppID     string
	ReleaseID string
}

func NewJobSpec(typ, appID, releaseID string) *JobSpec {
	return &JobSpec{
		JobType:   typ,
		AppID:     appID,
		ReleaseID: releaseID,
	}
}

type HostJobSpec struct {
	*JobSpec
	HostID string
}

func NewHostJobSpec(typ, appID, releaseID, hostID string) *HostJobSpec {
	return &HostJobSpec{
		JobSpec: NewJobSpec(typ, appID, releaseID),
		HostID:  hostID,
	}
}

type JobRequest struct {
	*Job
	RequestType JobRequestType
}

func NewJobRequest(requestType JobRequestType, typ, appID, releaseID, hostID, jobID string) *JobRequest {
	return &JobRequest{
		Job:         NewJob(typ, appID, releaseID, hostID, jobID),
		RequestType: requestType,
	}
}

type Job struct {
	*HostJobSpec
	JobID string

	restarts  int
	timer     *time.Timer
	timerMtx  sync.Mutex
	startedAt time.Time
}

func NewJob(typ, appID, releaseID, hostID, id string) *Job {
	return &Job{
		HostJobSpec: NewHostJobSpec(typ, appID, releaseID, hostID),
		JobID:       id,
	}
}

type jobTypeMap map[string]map[jobKey]*Job

func (m jobTypeMap) Add(job *Job) *Job {
	jobs, ok := m[job.JobType]
	if !ok {
		jobs = make(map[jobKey]*Job)
		m[job.JobType] = jobs
	}
	jobs[jobKey{job.HostID, job.JobID}] = job
	return job
}

func (m jobTypeMap) Remove(job *Job) {
	if jobs, ok := m[job.JobType]; ok {
		j := jobs[jobKey{job.HostID, job.JobID}]
		// cancel job restarts
		j.timerMtx.Lock()
		if j.timer != nil {
			j.timer.Stop()
			j.timer = nil
		}
		j.timerMtx.Unlock()
		delete(jobs, jobKey{job.HostID, job.JobID})
	}
}

func (m jobTypeMap) Get(typ, host, id string) *Job {
	return m[typ][jobKey{host, id}]
}
