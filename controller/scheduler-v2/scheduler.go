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
		jobSync:          make(chan interface{}, eventBufferSize),
		formationSync:    make(chan interface{}, eventBufferSize),
		rectifyJobs:      make(chan interface{}, eventBufferSize),
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
	go func() {
		for {
			select {
			case <-jobTicker:
				s.jobSync <- struct{}{}
			case <-formationTicker:
				s.formationSync <- struct{}{}
			}
		}
	}()
	s.jobSync <- struct{}{}
	s.formationSync <- struct{}{}

	chanList := []chanHandler{
		{
			channel: s.stop,
			handler: nil,
		}, {
			channel: s.jobRequests,
			handler: s.jobRequestHandler,
		}, {
			channel: s.rectifyJobs,
			handler: s.rectifyJobsHandler,
		}, {
			channel: s.formationChange,
			handler: s.formationChangeHandler,
		}, {
			channel: s.formationSync,
			handler: s.formationSyncHandler,
		}, {
			channel: s.jobSync,
			handler: s.jobSyncHandler,
		},
	}

	for {
		handled := false
		for _, chHandler := range chanList {
			handled = s.handleChan(chHandler.channel, chHandler.handler)
			if handled {
				break
			}
		}
		if !handled {
			<-time.After(time.Second)
		}
	}
	return nil
}

func (s *Scheduler) SyncJobs() (err error) {
	log := s.log.New("fn", "Sync")

	defer func() {
		s.sendEvent(NewEvent(EventTypeClusterSync, err, nil))
		if err != nil {
			s.formationSync <- struct{}{}
		}
	}()

	drainChannel(s.jobSync)

	log.Info("getting host list")
	hosts, err := s.Hosts()
	if err != nil {
		log.Error("error getting host list", "err", err)
		return err
	}
	log.Info(fmt.Sprintf("got %d hosts", len(hosts)))

	inactiveJobs := make(map[string]*Job)
	for k, v := range s.jobs {
		inactiveJobs[k] = v
	}
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
				if _, ok := s.jobs[job.ID]; ok {
					log.Info("skipping known job")
					delete(inactiveJobs, job.ID)
					continue
				}

				log.Info("getting formation")
				f := s.formations.Get(appID, releaseID)
				if f == nil {
					err = fmt.Errorf("no such formation obtained")
					continue
				}
				log.Info("adding job")
				j := NewJob(f, jobType, h.ID(), job.ID, activeJob.StartedAt)
				s.AddJob(j, appName, utils.JobMetaFromMetadata(job.Metadata))
			}
		}
	}
	if err != nil {
		return err
	}

	// Any jobs left in inactiveJobs no longer exist on the cluster
	for jobID := range inactiveJobs {
		delete(s.jobs, jobID)
	}
	s.rectifyJobs <- struct{}{}

	return err
}

func (s *Scheduler) SyncFormations() (err error) {
	log := s.log.New("fn", "SyncFormations")

	defer s.sendEvent(NewEvent(EventTypeFormationSync, err, nil))

	drainChannel(s.formationSync)

	log.Info("getting formations")

	if len(s.formations) == 0 {
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
				err = s.updateFormation(f, app.Name)
			}
		}
	}
	return err
}

func (s *Scheduler) RectifyJobs() (err error) {
	log := s.log.New("fn", "RectifyJobs")

	defer s.sendEvent(NewEvent(EventTypeRectifyJobs, err, nil))

	drainChannel(s.rectifyJobs)

	fj := NewFormationJobs(s.jobs)

	for fKey := range fj {
		schedulerFormation, ok := s.formations[fKey]

		if !ok {
			err = fmt.Errorf("Job exists without formation")
			continue
		}

		schedulerProcs := schedulerFormation.Processes
		clusterProcs := fj.GetProcesses(fKey)

		if eq := reflect.DeepEqual(clusterProcs, schedulerProcs); !eq {
			log.Info("rectifying processes", "formation.key", fKey, "scheduler.formation.Processes", schedulerProcs, "cluster.formation.Processes", clusterProcs)
			schedulerFormation.Processes = clusterProcs

			s.formationChange <- &ct.ExpandedFormation{
				App:       schedulerFormation.App,
				Release:   schedulerFormation.Release,
				Artifact:  schedulerFormation.Artifact,
				Processes: schedulerProcs,
				UpdatedAt: time.Now(),
			}
		}
	}

	for fKey, schedulerFormation := range s.formations {
		if _, ok := fj[fKey]; !ok {
			schedulerProcs := schedulerFormation.Processes

			schedulerFormation.Processes = make(map[string]int)

			s.formationChange <- &ct.ExpandedFormation{
				App:       schedulerFormation.App,
				Release:   schedulerFormation.Release,
				Artifact:  schedulerFormation.Artifact,
				Processes: schedulerProcs,
				UpdatedAt: time.Now(),
			}
		}
	}

	if err != nil {
		s.formationSync <- struct{}{}
	}
	return err
}

func (s *Scheduler) FormationChange(ef *ct.ExpandedFormation) (err error) {
	log := s.log.New("fn", "FormationChange")

	defer func() {
		if err != nil {
			log.Error("error in FormationChange", "err", err)
			s.formationSync <- struct{}{}
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

func (s *Scheduler) updateFormation(controllerFormation *ct.Formation, appName string) error {
	log := s.log.New("fn", "getFormation")

	appID := controllerFormation.AppID
	releaseID := controllerFormation.ReleaseID

	localFormation := s.formations.Get(appID, releaseID)

	if localFormation != nil {
		s.formationChange <- &ct.ExpandedFormation{
			App:       localFormation.App,
			Release:   localFormation.Release,
			Artifact:  localFormation.Artifact,
			Processes: controllerFormation.Processes,
			UpdatedAt: time.Now(),
		}
	} else {
		release, err := s.GetRelease(releaseID)
		if err != nil {
			log.Error("error getting release", "err", err)
			return err
		}

		artifact, err := s.GetArtifact(release.ArtifactID)
		if err != nil {
			log.Error("error getting artifact", "err", err)
			return err
		}

		s.formationChange <- &ct.ExpandedFormation{
			App:       &ct.App{ID: appID, Name: appName},
			Release:   release,
			Artifact:  artifact,
			Processes: controllerFormation.Processes,
			UpdatedAt: time.Now(),
		}
	}
	return nil
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
type chanHandler struct {
	channel chan interface{}
	handler func(interface{}) error
}

func (s *Scheduler) jobRequestHandler(i interface{}) error {
	req, ok := i.(*JobRequest)
	if !ok {
		return fmt.Errorf("Failed to cast to JobRequest")
	}
	return s.HandleJobRequest(req)
}

func (s *Scheduler) rectifyJobsHandler(i interface{}) error {
	return s.RectifyJobs()
}

func (s *Scheduler) formationChangeHandler(i interface{}) error {
	fc, ok := i.(*ct.ExpandedFormation)
	if !ok {
		return fmt.Errorf("Failed to cast to ExpandedFormation")
	}
	return s.FormationChange(fc)
}

func (s *Scheduler) formationSyncHandler(i interface{}) error {
	return s.SyncFormations()
}

func (s *Scheduler) jobSyncHandler(i interface{}) error {
	return s.SyncJobs()
}

func (s *Scheduler) handleChan(ch chan interface{}, handler func(interface{}) error) bool {
	log := s.log.New("fn", "handleChan")

	select {
	case test := <-ch:
		if handler != nil {
			if err := handler(test); err != nil {
				log.Error("error performing task", "err", err)
			}
		}
		return true
	default:
		return false
	}
}

func drainChannel(ch chan interface{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
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
