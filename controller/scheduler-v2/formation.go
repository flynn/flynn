package main

import (
	"fmt"
	"math"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/host/types"
)

type Formations struct {
	formations map[utils.FormationKey]*Formation
}

func newFormations() *Formations {
	return &Formations{
		formations: make(map[utils.FormationKey]*Formation),
	}
}

func (fs *Formations) Get(appID, releaseID string) *Formation {
	if form, ok := fs.formations[utils.FormationKey{AppID: appID, ReleaseID: releaseID}]; ok {
		return form
	}
	return nil
}

func (fs *Formations) Add(f *Formation) *Formation {
	if existing, ok := fs.formations[f.key()]; ok {
		return existing
	}
	fs.formations[f.key()] = f
	return f
}

func (fs *Formations) RectifyAll() error {
	for _, f := range fs.formations {
		err := f.Rectify()
		if err != nil {
			return err
		}
	}
	return nil
}

type Formation struct {
	*ct.ExpandedFormation

	jobs jobTypeMap
	s    *Scheduler
}

func NewFormation(s *Scheduler, ef *ct.ExpandedFormation) *Formation {
	return &Formation{
		ExpandedFormation: ef,
		jobs:              make(jobTypeMap),
		s:                 s,
	}
}

func (f *Formation) key() utils.FormationKey {
	return utils.FormationKey{f.App.ID, f.Release.ID}
}

func (f *Formation) GetJobsForType(typ string) map[jobKey]*Job {
	return f.jobs[typ]
}

func (f *Formation) SetFormation(ef *ct.ExpandedFormation) {
	f.ExpandedFormation = ef
}

func (f *Formation) Rectify() error {
	log := f.s.log.New("fn", "rectify")
	log.Info("rectifying formation", "app.id", f.App.ID, "release.id", f.Release.ID)

	for t, expected := range f.Processes {
		actual := len(f.jobs[t])
		diff := expected - actual
		if diff > 0 {
			f.sendJobRequest(JobRequestTypeUp, diff, t, "", "")
		} else if diff < 0 {
			f.sendJobRequest(JobRequestTypeDown, -diff, t, "", "")
		}
	}

	// remove extraneous process types
	for t, jobs := range f.jobs {
		// ignore jobs that don't have a type
		if t == "" {
			continue
		}

		if _, exists := f.Processes[t]; !exists {
			f.sendJobRequest(JobRequestTypeDown, len(jobs), t, "", "")
		}
	}
	return nil
}

func (f *Formation) sendJobRequest(requestType JobRequestType, numJobs int, typ string, hostID, jobID string) {
	for i := 0; i < numJobs; i++ {
		f.s.jobRequests <- NewJobRequest(requestType, typ, f.App.ID, f.Release.ID, hostID, jobID)
	}
}

func (f *Formation) handleJobRequest(req *JobRequest) (err error) {
	log := f.s.log.New("fn", "handleJobRequest")
	defer func() {
		if err != nil {
			log.Error("error handling job request", "error", err)
		}
	}()

	switch req.RequestType {
	case JobRequestTypeUp:
		_, err = f.startJob(req)
	case JobRequestTypeDown:
		err = f.stopJob(req)
	default:
		return fmt.Errorf("Unknown job request type")
	}
	return err
}

func (f *Formation) startJob(req *JobRequest) (job *Job, err error) {
	log := f.s.log.New("fn", "startJob")
	defer func() {
		if err != nil {
			log.Error("error starting job", "error", err)
		} else {
			log.Info("started job", "host.id", job.HostID, "job.type", job.JobType, "job.id", job.JobID)
		}
		f.s.sendEvent(NewEvent(EventTypeJobStart, err, job))
	}()
	h, err := f.findBestHost(req.JobType, req.HostID)
	if err != nil {
		return nil, err
	}

	hostJob := f.configureJob(req.JobType, h.ID())

	// Provision a data volume on the host if needed.
	if f.Release.Processes[req.JobType].Data {
		if err := utils.ProvisionVolume(h, hostJob); err != nil {
			return nil, err
		}
	}

	if err := h.AddJob(hostJob); err != nil {
		return nil, err
	}
	job, err = f.s.AddJob(
		NewJob(req.JobType, f.App.ID, f.Release.ID, h.ID(), hostJob.ID),
		f.App.Name,
		utils.JobMetaFromMetadata(hostJob.Metadata),
	)
	return job, err
}

func (f *Formation) stopJob(req *JobRequest) (err error) {
	log := f.s.log.New("fn", "stopJob")
	defer func() {
		if err != nil {
			log.Error("error stopping job", "error", err)
		}
		f.s.sendEvent(NewEvent(EventTypeJobStop, err, nil))
	}()
	h, err := f.s.Host(req.HostID)
	if err != nil {
		return err
	}

	if err := h.StopJob(req.JobID); err != nil {
		return err
	}
	f.s.RemoveJob(req.JobID)
	return nil
}

func (f *Formation) configureJob(typ, hostID string) *host.Job {
	return utils.JobConfig(&ct.ExpandedFormation{
		App:      &ct.App{ID: f.App.ID, Name: f.App.Name},
		Release:  f.Release,
		Artifact: f.Artifact,
	}, typ, hostID)
}

func (f *Formation) findBestHost(typ, hostID string) (utils.HostClient, error) {
	hosts, err := f.s.Hosts()
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("scheduler: no online hosts")
	}

	if hostID == "" {
		hostMap := f.getHostMap(typ)
		var minCount int = math.MaxInt32
		for _, host := range hosts {
			jobCount := hostMap[host.ID()]
			if jobCount < minCount {
				minCount = jobCount
				hostID = host.ID()
			}
		}
	}
	if hostID == "" {
		return nil, fmt.Errorf("no host found")
	}
	h, err := f.s.Host(hostID)
	if err != nil {
		return nil, err
	}
	return h, nil
}

func (f *Formation) getHostMap(typ string) map[string]int {
	hostMap := make(map[string]int)
	for _, j := range f.jobs[typ] {
		hostMap[j.HostID]++
	}
	return hostMap
}

func (f *Formation) jobType(job *host.Job) string {
	if job.Metadata["flynn-controller.app"] != f.App.ID ||
		job.Metadata["flynn-controller.release"] != f.Release.ID {
		return ""
	}
	return job.Metadata["flynn-controller.type"]
}
