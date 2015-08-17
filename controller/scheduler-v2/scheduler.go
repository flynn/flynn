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
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/stream"
)

const eventBufferSize int = 1000

type Scheduler struct {
	utils.ControllerClient
	utils.ClusterClient
	log log15.Logger

	isLeader     bool
	changeLeader chan bool

	formations Formations
	hosts      map[string]utils.HostClient
	jobs       map[string]*Job

	hostEvents chan *host.Event

	listeners map[chan Event]struct{}
	listenMtx sync.RWMutex

	stop     chan interface{}
	stopOnce sync.Once

	rectifyJobs     chan interface{}
	syncHosts       chan interface{}
	syncJobs        chan interface{}
	syncFormations  chan interface{}
	hostChange      chan utils.HostClient
	formationChange chan *ct.ExpandedFormation
	jobRequests     chan *JobRequest
	putJobs         chan *ct.Job
}

func NewScheduler(cluster utils.ClusterClient, cc utils.ControllerClient) *Scheduler {
	return &Scheduler{
		ControllerClient: cc,
		ClusterClient:    cluster,
		isLeader:         false,
		changeLeader:     make(chan bool),
		log:              log15.New("component", "scheduler"),
		hosts:            make(map[string]utils.HostClient),
		jobs:             make(map[string]*Job),
		formations:       make(Formations),
		hostEvents:       make(chan *host.Event, eventBufferSize),
		listeners:        make(map[chan Event]struct{}),
		stop:             make(chan interface{}),
		rectifyJobs:      make(chan interface{}, 1),
		syncHosts:        make(chan interface{}, 1),
		syncJobs:         make(chan interface{}, 1),
		syncFormations:   make(chan interface{}, 1),
		formationChange:  make(chan *ct.ExpandedFormation, eventBufferSize),
		hostChange:       make(chan utils.HostClient, eventBufferSize),
		jobRequests:      make(chan *JobRequest, eventBufferSize),
		putJobs:          make(chan *ct.Job, eventBufferSize),
	}
}

func main() {
	return
}

func (s *Scheduler) Run() error {
	log := s.log.New("fn", "Run")
	log.Info("starting scheduler loop")
	defer log.Info("exiting scheduler loop")

	tickChannel(s.syncJobs, 30*time.Second)
	tickChannel(s.syncFormations, time.Minute)
	tickChannel(s.syncHosts, 5*time.Minute)

	stream, err := s.StreamHosts(s.hostChange)
	if err != nil {
		return fmt.Errorf("Unable to stream hosts. Error: %v", err)
	}
	defer stream.Close()

	stream, err = s.StreamFormations(nil, s.formationChange)
	if err != nil {
		return fmt.Errorf("Unable to stream formations. Error: %v", err)
	}
	defer stream.Close()

	go s.RunPutJobs()

	for {
		// First handle events reconciling our state with the cluster
		select {
		case <-s.stop:
			return nil
		case isLeader := <-s.changeLeader:
			s.isLeader = isLeader
		case h := <-s.hostChange:
			s.followHost(h)
			continue
		case he := <-s.hostEvents:
			s.handleHostEvent(he)
			continue
		case ef := <-s.formationChange:
			s.FormationChange(ef)
			continue
		case <-time.After(50 * time.Millisecond):
		}

		// Then handle entropy minimizing sync events
		select {
		case <-s.syncHosts:
			s.SyncHosts()
		case <-s.syncFormations:
			s.SyncFormations()
		case <-s.syncJobs:
			s.SyncJobs()
		default:
		}

		// Then handle events that could mutate the cluster
		if s.isLeader {
			select {
			case req := <-s.jobRequests:
				s.HandleJobRequest(req)
			case <-s.rectifyJobs:
				s.RectifyJobs()
			default:
			}
		}
	}
	return nil
}

func (s *Scheduler) SyncHosts() (err error) {
	log := s.log.New("fn", "SyncHosts")

	defer func() {
		s.sendEvent(NewEvent(EventTypeHostSync, err, nil))
	}()

	log.Info("getting host list")
	hosts, err := s.Hosts()
	if err != nil {
		log.Error("error getting host list", "err", err)
		return err
	}
	for _, h := range hosts {
		s.followHost(h)
	}
	log.Info(fmt.Sprintf("got %d hosts", len(hosts)))
	return err
}

func (s *Scheduler) SyncJobs() (err error) {
	fLog := s.log.New("fn", "SyncJobs")

	defer func() {
		s.sendEvent(NewEvent(EventTypeClusterSync, err, nil))
	}()

	newSchedulerJobs := make(map[string]*Job)
	for _, h := range s.hosts {
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
			s.handleActiveJob(&activeJob)
			if j, ok := s.jobs[activeJob.Job.ID]; ok {
				newSchedulerJobs[j.JobID] = s.jobs[j.JobID]
			}
		}
	}

	s.jobs = newSchedulerJobs
	triggerChan(s.rectifyJobs)

	return err
}

func (s *Scheduler) SyncFormations() (err error) {
	log := s.log.New("fn", "SyncFormations")

	defer func() {
		s.sendEvent(NewEvent(EventTypeFormationSync, err, nil))
	}()

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
	if !s.isLeader {
		return errors.New("This scheduler is not the leader")
	}

	log := s.log.New("fn", "RectifyJobs")
	defer func() {
		if err != nil {
			log.Error("error rectifying jobs", "err", err)
		}
		s.sendEvent(NewEvent(EventTypeRectifyJobs, err, nil))
	}()

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

func (s *Scheduler) FormationChange(ef *ct.ExpandedFormation) (err error) {
	defer func() {
		s.sendEvent(NewEvent(EventTypeFormationChange, err, nil))
	}()

	s.changeFormation(ef)
	triggerChan(s.rectifyJobs)

	return nil
}

func (s *Scheduler) HandleJobRequest(req *JobRequest) (err error) {
	if !s.isLeader {
		return errors.New("This scheduler is not the leader")
	}

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

func (s *Scheduler) RunPutJobs() {
	strategy := attempt.Strategy{Delay: 100 * time.Millisecond, Total: time.Minute}
	for {
		job, ok := <-s.putJobs
		if !ok {
			return
		}
		strategy.Run(func() error {
			return s.PutJob(job)
		})
	}
}

func (s *Scheduler) ChangeLeader(isLeader bool) {
	s.changeLeader <- isLeader
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

func (s *Scheduler) followHost(h utils.HostClient) {
	log := s.log.New("fn", "followHost")
	_, ok := s.hosts[h.ID()]
	if !ok {
		log.Info("Following host", "host.id", h.ID())
		s.hosts[h.ID()] = h
		h.StreamEvents("all", s.hostEvents)
		triggerChan(s.syncFormations)
	}
}

func (s *Scheduler) handleHostEvent(he *host.Event) (err error) {
	log := s.log.New("fn", "handleHostEvent")

	defer func() {
		if err != nil {
			log.Error("error handling host event", "err", err)
		}
	}()

	log.Info("Handling job event", "job.event.type", he.Event)
	job, err := s.handleActiveJob(he.Job)

	switch he.Event {
	case host.JobEventCreate:
	case host.JobEventStart:
		s.sendEvent(NewEvent(EventTypeJobStart, err, job))
	case host.JobEventStop:
		s.sendEvent(NewEvent(EventTypeJobStop, err, nil))
	case host.JobEventError:
	}

	return err
}

func (s *Scheduler) handleActiveJob(activeJob *host.ActiveJob) (*Job, error) {
	job := activeJob.Job
	appID := job.Metadata["flynn-controller.app"]
	appName := job.Metadata["flynn-controller.app_name"]
	releaseID := job.Metadata["flynn-controller.release"]
	jobType := job.Metadata["flynn-controller.type"]

	log := s.log.New("fn", "handleActiveJob", "job.id", job.ID, "app.id", appID, "release.id", releaseID, "type", jobType)

	if appID == "" || releaseID == "" {
		return nil, errors.New("skipping due to lack of appID or releaseID")
	}
	j, ok := s.jobs[job.ID]
	if !ok {
		log.Info("getting formation")
		f := s.formations.Get(appID, releaseID)
		if f == nil {
			var cf *ct.Formation
			cf, err := s.GetFormation(appID, releaseID)
			if err != nil || cf == nil {
				return nil, errors.New("Job found with unknown formation")
			}
			f, err = s.updateFormation(cf)
			if err != nil || f == nil {
				return nil, errors.New("Unable to update formation")
			}
		}
		j = NewJob(f, jobType, activeJob.HostID, job.ID, activeJob.StartedAt)
	}
	s.SaveJob(j, appName, activeJob.Status, utils.JobMetaFromMetadata(job.Metadata))
	triggerChan(s.rectifyJobs)
	return j, nil
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

	var ef *ct.ExpandedFormation
	var err error

	if localFormation != nil {
		log.Info("Updating formation", "app.id", appID, "release.id", releaseID, "formation.processes", controllerFormation.Processes)
		ef = &ct.ExpandedFormation{
			App:       localFormation.App,
			Release:   localFormation.Release,
			Artifact:  localFormation.Artifact,
			Processes: controllerFormation.Processes,
			UpdatedAt: time.Now(),
		}
	} else {
		log.Info("Creating new formation", "app.id", appID, "release.id", releaseID, "formation.processes", controllerFormation.Processes)
		ef, err = utils.ExpandedFormationFromFormation(s, controllerFormation)
		if err != nil {
			return nil, err
		}
		ef.UpdatedAt = time.Now()
	}
	s.expandOmni(ef)
	return s.changeFormation(ef), nil
}

func (s *Scheduler) startJob(req *JobRequest) (err error) {
	log := s.log.New("fn", "startJob")
	defer func() {
		if err != nil {
			log.Error("error starting job", "err", err)
		}
	}()

	host, err := s.findBestHost(req.Type, req.HostID)
	if err != nil {
		s.jobRequests <- req
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
	log.Info("started job", "host.id", host.ID(), "job.type", req.Type, "job.id", config.ID)
	return err
}

func (s *Scheduler) stopJob(req *JobRequest) (err error) {
	log := s.log.New("fn", "stopJob")
	defer func() {
		if err != nil {
			log.Error("error stopping job", "err", err)
		}
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
		req.JobID = job.JobID
		s.jobRequests <- req
		return err
	}
	if err := host.StopJob(job.JobID); err != nil {
		return err
	}
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
	s.stopOnce.Do(func() {
		close(s.stop)
		close(s.putJobs)
	})
	return nil
}

func (s *Scheduler) Subscribe(events chan Event) stream.Stream {
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

func (s *Scheduler) SaveJob(job *Job, appName string, status host.JobStatus, metadata map[string]string) (*Job, error) {
	s.log.Info("Saving job", "job.id", job.JobID, "job.status", status)
	controllerState := "down"
	switch status {
	case host.StatusStarting:
		fallthrough
	case host.StatusRunning:
		s.jobs[job.JobID] = job
		controllerState = "up"
	default:
		delete(s.jobs, job.JobID)
	}
	s.putJobs <- controllerJobFromSchedulerJob(job, controllerState, metadata)
	return job, nil
}

func (s *Scheduler) Jobs() map[string]*Job {
	return s.jobs
}

func (s *Scheduler) expandOmni(ef *ct.ExpandedFormation) {
	release := ef.Release

	for typ, pt := range release.Processes {
		if pt.Omni {
			ef.Processes[typ] *= len(s.hosts)
		}
	}
}

func tickChannel(ch chan interface{}, d time.Duration) {
	ticker := time.Tick(d)

	go func() {
		for range ticker {
			triggerChan(ch)
		}
	}()
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

func (s *Stream) Err() error {
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
	EventTypeHostSync        EventType = "host-sync"
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
