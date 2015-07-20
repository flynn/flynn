package main

import (
	"errors"
	"fmt"
	"math"
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
	formations Formations

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
		formations:       make(Formations),
		formationChange:  make(chan *ct.ExpandedFormation, eventBufferSize),
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

	jobSync := time.Tick(30 * time.Second)
	formationSync := time.Tick(time.Minute)

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

		// next, handle sync and formation change events
		select {
		case <-s.stop:
			return nil
		case <-jobSync:
			log.Info("starting cluster sync")
			if err := s.SyncJobs(); err != nil {
				log.Error("error performing cluster sync", "err", err)
				continue
			}
		case <-formationSync:
			s.SyncFormations()
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

func (s *Scheduler) SyncJobs() (err error) {
	log := s.log.New("fn", "Sync")

	defer func() {
		s.sendEvent(NewEvent(EventTypeClusterSync, err, nil))
	}()

	s.SyncFormations()

	log.Info("getting host list")
	hosts, err := s.Hosts()
	if err != nil {
		log.Error("error getting host list", "err", err)
		return err
	}
	log.Info(fmt.Sprintf("got %d hosts", len(hosts)))

	unSyncedJobs := make(map[string]*Job)
	for k, v := range s.jobs {
		unSyncedJobs[k] = v
	}
	fc := make(formationJobs)
	for _, h := range hosts {
		log = log.New("host.id", h.ID())
		log.Info("getting jobs list")
		var activeJobs map[string]host.ActiveJob
		activeJobs, err = h.ListJobs()
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

				log = log.New("job.id", job.ID, "app.id", appID, "release.id", releaseID, "type", jobType)

				if appID == "" || releaseID == "" {
					log.Info("skipping due to lack of appID or releaseID")
					continue
				}
				if j, ok := s.jobs[job.ID]; ok {
					log.Info("skipping known job")
					delete(unSyncedJobs, job.ID)
					fc.AddJob(j)
					continue
				}

				log.Info("getting formation")
				f, err := s.getFormation(appID, appName, releaseID)
				if err != nil {
					log.Error("error getting formation", "err", err)
					continue
				}
				log.Info("adding job")
				j := NewJob(f, jobType, h.ID(), job.ID)
				s.AddJob(j, appName, utils.JobMetaFromMetadata(job.Metadata))
				fc.AddJob(j)
			}
		}
	}
	for jobID := range unSyncedJobs {
		delete(s.jobs, jobID)
	}
	return s.rectifyFormations(fc)
}

func (s *Scheduler) SyncFormations() (err error) {
	log := s.log.New("fn", "SyncFormations")

	log.Info("getting formations")

	if len(s.formations) == 0 {
		log.Info("getting apps")
		apps, err := s.AppList()
		if err != nil {
			return err
		}
		for _, app := range apps {
			fs, err := s.FormationList(app.ID)
			if err != nil {
				return err
			}
			for _, f := range fs {
				form, err := s.getFormation(app.ID, app.Name, f.ReleaseID)
				if err != nil {
					return err
				}
				s.formations[form.key()] = form
			}
		}
	}
	return err
}

func (s *Scheduler) rectifyFormations(fc formationJobs) (err error) {
	for fKey, formation := range s.formations {
		for typ, count := range formation.Processes {
			diff := count - fc[fKey][typ]

			if diff > 0 {
				for i := 0; i < diff; i++ {
					s.jobRequests <- NewJobRequest(formation, JobRequestTypeUp, typ, "", "")
				}
			} else if diff < 0 {
				for i := 0; i < -diff; i++ {
					s.jobRequests <- NewJobRequest(formation, JobRequestTypeDown, typ, "", "")
				}
			}
			delete(fc[fKey], typ)
		}
		if len(fc[fKey]) == 0 {
			delete(fc, fKey)
		} else {
			err = fmt.Errorf("Unknown job type in formation %v", fc[fKey])
		}
	}
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
				log.Error("error getting release", "err", err)
				return nil, err
			}
			releases[release.ID] = release
		}

		artifact := artifacts[release.ArtifactID]
		if artifact == nil {
			artifact, err = s.GetArtifact(release.ArtifactID)
			if err != nil {
				log.Error("error getting artifact", "err", err)
				return nil, err
			}
			artifacts[artifact.ID] = artifact
		}

		formation, err := s.GetFormation(appID, releaseID)
		if err != nil {
			log.Error("error getting formation", "err", err)
			return nil, err
		}

		f = NewFormation(&ct.ExpandedFormation{
			App:       &ct.App{ID: appID, Name: appName},
			Release:   release,
			Artifact:  artifact,
			Processes: formation.Processes,
		})
		log.Info("adding formation")
		f = s.formations.Add(f)
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
	var diff map[string]int
	if f == nil {
		log.Info("creating new formation")
		f = s.formations.Add(NewFormation(ef))
		diff = f.Processes
	} else {
		diff = f.Update(ef.Processes)
	}
	for typ, n := range diff {
		if n > 0 {
			for i := 0; i < n; i++ {
				s.jobRequests <- NewJobRequest(f, JobRequestTypeUp, typ, "", "")
			}
		} else if n < 0 {
			for i := 0; i < -n; i++ {
				s.jobRequests <- NewJobRequest(f, JobRequestTypeDown, typ, "", "")
			}
		}
	}
	return nil
}

func (s *Scheduler) HandleJobRequest(req *JobRequest) (err error) {
	log := s.log.New("fn", "HandleJobRequest")
	defer func() {
		if err != nil {
			log.Error("error handling job request", "err", err)
		}
	}()
	switch req.RequestType {
	case JobRequestTypeUp:
		err = s.startJob(req)
	case JobRequestTypeDown:
		err = s.stopJob(req)
	default:
		err = fmt.Errorf("unknown job request type: %s", req.RequestType)
	}
	return
}

func (s *Scheduler) startJob(req *JobRequest) (err error) {
	log := s.log.New("fn", "startJob")
	var job *Job
	defer func() {
		if err != nil {
			log.Error("error starting job", "err", err)
		}
		s.sendEvent(NewEvent(EventTypeJobStart, err, job))
	}()

	host, err := s.findBestHost(req.Type, req.HostID)
	if err != nil {
		return err
	}

	config := jobConfig(req, host.ID())

	// Provision a data volume on the host if needed.
	if req.needsVolume() {
		if err := utils.ProvisionVolume(host, config); err != nil {
			return err
		}
	}

	if err := host.AddJob(config); err != nil {
		return err
	}
	job, err = s.AddJob(
		NewJob(req.Job.Formation, req.Type, host.ID(), config.ID),
		req.Job.Formation.App.Name,
		utils.JobMetaFromMetadata(config.Metadata),
	)
	if err != nil {
		return err
	}
	log.Info("started job", "host.id", job.HostID, "job.type", job.Type, "job.id", job.JobID)
	return err
}

func (s *Scheduler) stopJob(req *JobRequest) (err error) {
	log := s.log.New("fn", "stopJob")
	defer func() {
		if err != nil {
			log.Error("error stopping job", "err", err)
		}
		s.sendEvent(NewEvent(EventTypeJobStop, err, nil))
	}()
	//FIXME: HostID and JobID are most likely empty right now
	host, err := s.Host(req.HostID)
	if err != nil {
		return err
	}
	if err := host.StopJob(req.JobID); err != nil {
		return err
	}
	s.RemoveJob(req.JobID)
	return nil
}

func jobConfig(req *JobRequest, hostID string) *host.Job {
	return utils.JobConfig(req.Job.Formation.ExpandedFormation, req.Type, hostID)
}

func (s *Scheduler) findBestHost(typ, hostID string) (utils.HostClient, error) {
	hosts, err := s.Hosts()
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, errors.New("no hosts found")
	}

	if hostID == "" {
		counts := s.hostJobCounts(typ)
		var minCount int = math.MaxInt32
		for _, host := range hosts {
			count := counts[host.ID()]
			if count < minCount {
				minCount = count
				hostID = host.ID()
			}
		}
	}
	return s.Host(hostID)
}

func (s *Scheduler) hostJobCounts(typ string) map[string]int {
	counts := make(map[string]int)
	for _, job := range s.jobs {
		if job.Type != typ {
			continue
		}
		counts[job.HostID]++
	}
	return counts
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
	s.jobs[job.JobID] = job
	s.PutJob(controllerJobFromSchedulerJob(job, "up", metadata))
	return job, nil
}

func (s *Scheduler) RemoveJob(jobID string) {
	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	s.PutJob(controllerJobFromSchedulerJob(job, "down", make(map[string]string)))
	delete(s.jobs, jobID)
}

func (s *Scheduler) Jobs() map[string]*Job {
	jobs := make(map[string]*Job)
	for id, job := range s.jobs {
		jobs[id] = job
	}
	return jobs
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
