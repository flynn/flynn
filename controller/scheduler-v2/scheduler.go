package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/host/types"
)

const eventBufferSize int = 1000

type Scheduler struct {
	utils.ControllerClient
	utils.ClusterClient
	log        log15.Logger
	formations *Formations

	jobs map[string]*Job

	listeners map[chan Event]struct{}
	listenMtx sync.RWMutex

	stop     chan struct{}
	stopOnce sync.Once

	formationChange chan *ct.ExpandedFormation
	jobRequests     chan *JobRequest

	validJobStatuses map[host.JobStatus]bool
}

func NewScheduler(cluster utils.ClusterClient, cc utils.ControllerClient) *Scheduler {
	return &Scheduler{
		ControllerClient: cc,
		ClusterClient:    cluster,
		log:              log15.New("component", "scheduler"),
		jobs:             make(map[string]*Job),
		listeners:        make(map[chan Event]struct{}),
		stop:             make(chan struct{}),
		formations:       newFormations(),
		formationChange:  make(chan *ct.ExpandedFormation, 1),
		jobRequests:      make(chan *JobRequest, eventBufferSize),
		validJobStatuses: map[host.JobStatus]bool{
			host.StatusStarting: true,
			host.StatusRunning:  true,
		},
	}
}

func main() {
	return
}

func (s *Scheduler) Run() error {
	log := s.log.New("fn", "Run")
	log.Info("starting scheduler loop")
	defer log.Info("exiting scheduler loop")

	for {
		// first, check if we should stop or process pending job events
		select {
		case <-s.stop:
			return nil
		case req := <-s.jobRequests:
			s.HandleJobRequest(req)
			continue
		default:
		}

		log.Info("starting cluster sync")
		if err := s.Sync(); err != nil {
			log.Error("error performing cluster sync", "err", err)
			continue
		}

		log.Info("starting watching events")
		select {
		case <-s.stop:
			return nil
		case fc := <-s.formationChange:
			if err := s.FormationChange(fc); err != nil {
				log.Error("error performing formation change", "err", err)
				continue
			}
		case <-time.After(time.Second):
		}
	}
	return nil
}

func (s *Scheduler) Sync() (err error) {
	log := s.log.New("fn", "Sync")

	defer func() {
		s.sendEvent(NewEvent(EventTypeClusterSync, err, nil))
	}()

	log.Info("getting host list")
	hosts, err := s.Hosts()
	if err != nil {
		log.Error("error getting host list", "err", err)
		return err
	}
	log.Info(fmt.Sprintf("got %d hosts", len(hosts)))

	for _, h := range hosts {
		log = log.New("host_id", h.ID())
		log.Info("getting jobs list")
		activeJobs, err := h.ListJobs()
		if err != nil {
			log.Error("error getting jobs list", "err", err)
			continue
		}
		log.Info("active jobs", "count", len(activeJobs))
		for _, activeJob := range activeJobs {
			if s.validJobStatuses[activeJob.Status] {
				job := activeJob.Job
				appID := job.Metadata["flynn-controller.app"]
				appName := job.Metadata["flynn-controller.app_name"]
				releaseID := job.Metadata["flynn-controller.release"]
				jobType := job.Metadata["flynn-controller.type"]
				log.Info("adding job", "host.id", h.ID(), "job.id", job.ID, "app.id", appID, "release.id", releaseID, "type", jobType)

				if appID == "" || releaseID == "" {
					continue
				}
				if _, ok := s.jobs[job.ID]; ok {
					continue
				}

				s.AddJob(NewJob(jobType, appID, releaseID, h.ID(), job.ID), appName, utils.JobMetaFromMetadata(job.Metadata))
			}
		}
	}
	if err != nil {
		return err
	}
	err = s.formations.RectifyAll()
	return err
}

func (s *Scheduler) getFormation(appID, appName, releaseID string) (*Formation, error) {
	log := s.log.New("fn", "getFormation")

	artifacts := make(map[string]*ct.Artifact)
	releases := make(map[string]*ct.Release)

	f := s.formations.Get(appID, releaseID)
	if f == nil {
		release := releases[releaseID]
		var err error
		if release == nil {
			release, err = s.GetRelease(releaseID)
			if err != nil {
				log.Error("at", "getRelease", "status", "error", "err", err)
				return nil, err
			}
			releases[release.ID] = release
		}

		artifact := artifacts[release.ArtifactID]
		if artifact == nil {
			artifact, err := s.GetArtifact(release.ArtifactID)
			if err != nil {
				log.Error("at", "getArtifact", "status", "error", "err", err)
				return nil, err
			}
			artifacts[artifact.ID] = artifact
		}

		formation, err := s.GetFormation(appID, releaseID)
		if err != nil {
			log.Error("at", "getFormation", "status", "error", "err", err)
			return nil, err
		}

		f = NewFormation(s, &ct.ExpandedFormation{
			App:       &ct.App{ID: appID, Name: appName},
			Release:   release,
			Artifact:  artifact,
			Processes: formation.Processes,
		})
		log.Info("at", "addFormation")
		f = s.formations.Add(f)
	}
	if f == nil {
		return nil, fmt.Errorf("no formation found")
	}
	return f, nil
}

func (s *Scheduler) FormationChange(ef *ct.ExpandedFormation) (err error) {
	log := s.log.New("fn", "FormationChange")

	defer func() {
		if err != nil {
			log.Error("error in FormationChange", "err", err)
		}
		s.sendEvent(NewEvent(EventTypeFormationChange, err, nil))
	}()

	f := s.formations.Get(ef.App.ID, ef.Release.ID)
	if f != nil {
		f.SetFormation(ef)
	} else {
		log.Info("creating new formation")
		f = NewFormation(s, ef)
		s.formations.Add(f)
	}
	return f.Rectify()
}

func (s *Scheduler) HandleJobRequest(req *JobRequest) error {
	f := s.formations.Get(req.AppID, req.ReleaseID)
	f.handleJobRequest(req)
	return nil
}

func (s *Scheduler) Stop() error {
	s.log.Info("stopping scheduler loop", "fn", "Stop")
	s.stopOnce.Do(func() { close(s.stop) })
	return nil
}

func (s *Scheduler) Subscribe(events chan Event) *Stream {
	s.log.Info("adding subscriber", "fn", "Subscribe")
	s.listenMtx.Lock()
	defer s.listenMtx.Unlock()
	s.listeners[events] = struct{}{}
	return &Stream{s, events}
}

func (s *Scheduler) Unsubscribe(events chan Event) {
	s.log.Info("removing subscriber", "fn", "Unsubscribe")
	s.listenMtx.Lock()
	defer s.listenMtx.Unlock()
	delete(s.listeners, events)
}

func (s *Scheduler) AddJob(job *Job, appName string, metadata map[string]string) (*Job, error) {
	f, err := s.getFormation(job.AppID, appName, job.ReleaseID)
	if err != nil {
		return nil, err
	}
	job = f.jobs.Add(job)
	s.jobs[job.JobID] = job
	s.PutJob(controllerJobFromSchedulerJob(job, "up", metadata))
	return job, nil
}

func (s *Scheduler) RemoveJob(jobID string) {
	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	f := s.formations.Get(job.AppID, job.ReleaseID)
	f.jobs.Remove(job)
	s.PutJob(controllerJobFromSchedulerJob(job, "down", make(map[string]string)))
	delete(s.jobs, jobID)
}

type Stream struct {
	s      *Scheduler
	events chan Event
}

func (s *Stream) Close() error {
	s.s.Unsubscribe(s.events)
	return nil
}

func (s *Scheduler) sendEvent(event Event) {
	s.listenMtx.RLock()
	defer s.listenMtx.RUnlock()
	s.log.Info("sending event to listeners", "event.type", event.Type(), "listeners.count", len(s.listeners))
	for ch := range s.listeners {
		// TODO: handle slow listeners
		ch <- event
	}
}

type Event interface {
	Type() EventType
	Err() error
}

type EventType string

const (
	EventTypeDefault         EventType = "default"
	EventTypeClusterSync     EventType = "cluster-sync"
	EventTypeFormationChange EventType = "formation-change"
	EventTypeJobStart        EventType = "start-job"
	EventTypeJobStop         EventType = "stop-job"
)

type DefaultEvent struct {
	err error
	typ EventType
}

func (de *DefaultEvent) Err() error {
	return de.err
}

func (de *DefaultEvent) Type() EventType {
	return de.typ
}

type JobStartEvent struct {
	Event
	Job *Job
}

func NewEvent(typ EventType, err error, data interface{}) Event {
	switch typ {
	case EventTypeJobStart:
		job, ok := data.(*Job)
		if !ok {
			job = nil
		}
		return &JobStartEvent{Event: &DefaultEvent{err: err, typ: typ}, Job: job}
	default:
		return &DefaultEvent{err: err, typ: typ}
	}
}

// TODO refactor `state` to JobStatus type and consolidate statuses across scheduler/controller/host
func controllerJobFromSchedulerJob(job *Job, state string, metadata map[string]string) *ct.Job {
	return &ct.Job{
		ID:        job.JobID,
		AppID:     job.AppID,
		ReleaseID: job.ReleaseID,
		Type:      job.JobType,
		State:     state,
		Meta:      metadata,
	}
}
