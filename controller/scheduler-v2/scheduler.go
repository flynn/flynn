package main

import (
	"errors"
	"fmt"
	"math"
	"reflect"
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

	stop     chan interface{}
	stopOnce sync.Once

	jobSync         chan interface{}
	formationSync   chan interface{}
	rectifyJobs     chan interface{}
	formationChange chan interface{}
	jobRequests     chan interface{}

	validJobStatuses map[host.JobStatus]bool
}

func NewScheduler(cluster utils.ClusterClient, cc utils.ControllerClient) *Scheduler {
	return &Scheduler{
		ControllerClient: cc,
		ClusterClient:    cluster,
		log:              log15.New("component", "scheduler"),
		jobs:             make(map[string]*Job),
		listeners:        make(map[chan Event]struct{}),
		formations:       make(Formations),
		stop:             make(chan interface{}),
		jobSync:          make(chan interface{}, 1),
		formationSync:    make(chan interface{}, 1),
		rectifyJobs:      make(chan interface{}, 1),
		formationChange:  make(chan interface{}, eventBufferSize),
		jobRequests:      make(chan interface{}, eventBufferSize),
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

	jobTicker := time.Tick(30 * time.Second)
	formationTicker := time.Tick(time.Minute)
	formationTimeout := time.After(time.Second)
	go func() {
		for {
			select {
			case <-jobTicker:
				triggerChan(s.jobSync)
			case <-formationTicker:
				triggerChan(s.formationSync)
			case <-formationTimeout:
				triggerChan(s.formationSync)
			}
		}
	}()
	triggerChan(s.jobSync)

	for {
		select {
		case <-s.stop:
			return nil
		case req := <-s.jobRequests:
			s.jobRequestHandler(req)
		case <-s.rectifyJobs:
			s.RectifyJobs()
		case ef := <-s.formationChange:
			s.formationChangeHandler(ef)
		case <-s.formationSync:
			s.SyncFormations()
		case <-s.jobSync:
			s.SyncJobs()
		}
	}
	return nil
}

func (s *Scheduler) SyncJobs() (err error) {
	fLog := s.log.New("fn", "Sync")

	defer func() {
		s.sendEvent(NewEvent(EventTypeClusterSync, err, nil))
	}()

	fLog.Info("getting host list")
	hosts, err := s.Hosts()
	if err != nil {
		fLog.Error("error getting host list", "err", err)
		return err
	}
	fLog.Info(fmt.Sprintf("got %d hosts", len(hosts)))

	inactiveJobs := make(map[string]*Job)
	for k, v := range s.jobs {
		inactiveJobs[k] = v
	}
	for _, h := range hosts {
		hLog := fLog.New("host.id", h.ID())
		hLog.Info("getting jobs list")
		var activeJobs map[string]host.ActiveJob
		activeJobs, err = h.ListJobs()
		if err != nil {
			hLog.Error("error getting jobs list", "err", err)
			continue
		}
		hLog.Info("active jobs", "count", len(activeJobs))
		for _, activeJob := range activeJobs {
			if s.validJobStatuses[activeJob.Status] {
				job := activeJob.Job
				appID := job.Metadata["flynn-controller.app"]
				appName := job.Metadata["flynn-controller.app_name"]
				releaseID := job.Metadata["flynn-controller.release"]
				jobType := job.Metadata["flynn-controller.type"]

				log := hLog.New("job.id", job.ID, "app.id", appID, "release.id", releaseID, "type", jobType)

				if appID == "" || releaseID == "" {
					log.Info("skipping due to lack of appID or releaseID")
					continue
				}
				if _, ok := s.jobs[job.ID]; ok {
					log.Info("skipping known job")
					delete(inactiveJobs, job.ID)
					continue
				}

				log.Info("getting formation")
				f := s.formations.Get(appID, releaseID)
				if f == nil {
					var cf *ct.Formation
					cf, err = s.GetFormation(appID, releaseID)
					if cf == nil {
						err = fmt.Errorf("Job found with unknown formation")
						continue
					}
					f, err = s.updateFormation(cf)
					if f == nil {
						err = fmt.Errorf("Unable to update formation")
						continue
					}
				}
				log.Info("adding job")
				j := NewJob(f, jobType, h.ID(), job.ID, activeJob.StartedAt)
				s.AddJob(j, appName, utils.JobMetaFromMetadata(job.Metadata))
			}
		}
	}

	// Any jobs left in inactiveJobs no longer exist on the cluster
	for jobID := range inactiveJobs {
		delete(s.jobs, jobID)
	}
	triggerChan(s.rectifyJobs)

	return err
}

func (s *Scheduler) SyncFormations() (err error) {
	log := s.log.New("fn", "SyncFormations")

	defer s.sendEvent(NewEvent(EventTypeFormationSync, err, nil))

	log.Info("getting formations")
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
			_, err = s.updateFormation(f)
		}
	}
	triggerChan(s.rectifyJobs)
	return err
}

func (s *Scheduler) RectifyJobs() (err error) {
	log := s.log.New("fn", "RectifyJobs")

	defer s.sendEvent(NewEvent(EventTypeRectifyJobs, err, nil))

	fj := NewFormationJobs(s.jobs)

	for fKey := range fj {
		schedulerFormation, ok := s.formations[fKey]

		if !ok {
			cf, err := s.GetFormation(fKey.AppID, fKey.ReleaseID)
			if err != nil {
				log.Error("Job exists without formation")
				continue
			}
			schedulerFormation, err = s.updateFormation(cf)
		}

		schedulerProcs := schedulerFormation.Processes
		clusterProcs := fj.GetProcesses(fKey)

		if eq := reflect.DeepEqual(clusterProcs, schedulerProcs); !eq {
			log.Info("Updating processes", "formation.processes", schedulerProcs, "cluster.processes", clusterProcs)
			schedulerFormation.Processes = clusterProcs

			diff := schedulerFormation.Update(schedulerProcs)
			s.sendDiffRequests(schedulerFormation, diff)
		}
	}

	for fKey, schedulerFormation := range s.formations {
		if _, ok := fj[fKey]; !ok {
			log.Info("Re-asserting processes", "formation.processes", schedulerFormation.Processes, "formation.jobs", fj)
			s.sendDiffRequests(schedulerFormation, schedulerFormation.Processes)
		}
	}

	return err
}

func (s *Scheduler) sendDiffRequests(f *Formation, diff map[string]int) {
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
}

func (s *Scheduler) FormationChange(ef *ct.ExpandedFormation) (err error) {
	defer s.sendEvent(NewEvent(EventTypeFormationChange, err, nil))

	s.changeFormation(ef)
	triggerChan(s.rectifyJobs)

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

func (s *Scheduler) changeFormation(ef *ct.ExpandedFormation) *Formation {
	f := s.formations.Get(ef.App.ID, ef.Release.ID)
	if f == nil {
		f = s.formations.Add(NewFormation(ef))
	} else {
		f.Processes = ef.Processes
	}
	return f
}

func (s *Scheduler) updateFormation(controllerFormation *ct.Formation) (*Formation, error) {
	log := s.log.New("fn", "getFormation")

	appID := controllerFormation.AppID
	releaseID := controllerFormation.ReleaseID

	localFormation := s.formations.Get(appID, releaseID)

	if localFormation != nil {
		log.Info("Updating formation", "app.id", appID, "release.id", releaseID, "formation.processes", controllerFormation.Processes)
		return s.changeFormation(&ct.ExpandedFormation{
			App:       localFormation.App,
			Release:   localFormation.Release,
			Artifact:  localFormation.Artifact,
			Processes: controllerFormation.Processes,
			UpdatedAt: time.Now(),
		}), nil
	} else {
		app, err := s.GetApp(appID)
		if err != nil {
			log.Error("error getting app", "err", err)
			return nil, err
		}

		release, err := s.GetRelease(releaseID)
		if err != nil {
			log.Error("error getting release", "err", err)
			return nil, err
		}

		artifact, err := s.GetArtifact(release.ArtifactID)
		if err != nil {
			log.Error("error getting artifact", "err", err)
			return nil, err
		}

		log.Info("Creating new formation", "app.id", appID, "release.id", releaseID, "formation.processes", controllerFormation.Processes)
		return s.changeFormation(&ct.ExpandedFormation{
			App:       app,
			Release:   release,
			Artifact:  artifact,
			Processes: controllerFormation.Processes,
			UpdatedAt: time.Now(),
		}), nil
	}
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
		NewJob(req.Job.Formation, req.Type, host.ID(), config.ID, time.Now()),
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
	var job *Job
	if req.JobID == "" {
		formJobs := NewFormationJobs(s.jobs)
		formJob := formJobs[utils.FormationKey{AppID: req.AppID, ReleaseID: req.ReleaseID}]
		typJobs := formJob[req.Type]

		if len(typJobs) == 0 {
			return fmt.Errorf("No running jobs of type %q", req.Type)
		}
		job = typJobs[0]
		startedAt := job.startedAt
		for _, j := range typJobs {
			if j.startedAt.After(startedAt) {
				job = j
				startedAt = j.startedAt
			}
		}
	} else {
		var ok bool
		job, ok = s.jobs[req.JobID]
		if !ok {
			return fmt.Errorf("Could not stop job with ID %q", req.JobID)
		}
	}
	host, err := s.Host(job.HostID)
	if err != nil {
		return err
	}
	if err := host.StopJob(job.JobID); err != nil {
		return err
	}
	s.RemoveJob(job.JobID)
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

// Channel handlers
func (s *Scheduler) jobRequestHandler(i interface{}) error {
	req, ok := i.(*JobRequest)
	if !ok {
		return fmt.Errorf("Failed to cast to JobRequest")
	}
	return s.HandleJobRequest(req)
}

func (s *Scheduler) formationChangeHandler(i interface{}) error {
	fc, ok := i.(*ct.ExpandedFormation)
	if !ok {
		return fmt.Errorf("Failed to cast to ExpandedFormation")
	}
	return s.FormationChange(fc)
}

func triggerChan(ch chan interface{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
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
	EventTypeFormationSync   EventType = "formation-sync"
	EventTypeFormationChange EventType = "formation-change"
	EventTypeRectifyJobs     EventType = "rectify-jobs"
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
