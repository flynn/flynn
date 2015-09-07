package main

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/stream"
)

const eventBufferSize int = 1000

type Scheduler struct {
	utils.ControllerClient
	utils.ClusterClient
	log log15.Logger

	isLeader      bool
	changeLeader  chan bool
	backoffPeriod time.Duration

	formations    Formations
	hostStreams   map[string]stream.Stream
	jobs          map[string]*Job
	pendingStarts pendingJobs
	pendingStops  pendingJobs

	hostEvents chan *host.Event

	listeners map[chan Event]struct{}
	listenMtx sync.RWMutex

	stop     chan struct{}
	stopOnce sync.Once

	rectifyJobs     chan struct{}
	syncJobs        chan struct{}
	syncFormations  chan struct{}
	hostChange      chan *discoverd.Event
	formationChange chan *ct.ExpandedFormation
	jobRequests     chan *JobRequest
	putJobs         chan *ct.Job
}

func NewScheduler(cluster utils.ClusterClient, cc utils.ControllerClient) *Scheduler {
	return &Scheduler{
		ControllerClient: cc,
		ClusterClient:    cluster,
		changeLeader:     make(chan bool),
		backoffPeriod:    getBackoffPeriod(),
		log:              log15.New("component", "scheduler"),
		hostStreams:      make(map[string]stream.Stream),
		jobs:             make(map[string]*Job),
		pendingStarts:    make(pendingJobs),
		pendingStops:     make(pendingJobs),
		formations:       make(Formations),
		hostEvents:       make(chan *host.Event, eventBufferSize),
		listeners:        make(map[chan Event]struct{}),
		stop:             make(chan struct{}),
		rectifyJobs:      make(chan struct{}, 1),
		syncJobs:         make(chan struct{}, 1),
		syncFormations:   make(chan struct{}, 1),
		formationChange:  make(chan *ct.ExpandedFormation, eventBufferSize),
		hostChange:       make(chan *discoverd.Event, eventBufferSize),
		jobRequests:      make(chan *JobRequest, eventBufferSize),
		putJobs:          make(chan *ct.Job, eventBufferSize),
	}
}

func main() {
	clusterClient := utils.ClusterClientWrapper(cluster.NewClient())
	controllerClient, err := controller.NewClient("", os.Getenv("AUTH_KEY"))
	if err != nil {
		shutdown.Fatal(err)
	}
	s := NewScheduler(clusterClient, controllerClient)

	hb, err := discoverd.AddServiceAndRegister("controller-scheduler", ":"+os.Getenv("PORT"))
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { hb.Close() })
	leaders := make(chan *discoverd.Instance)
	stream, err := discoverd.NewService("controller-scheduler").Leaders(leaders)
	shutdown.BeforeExit(func() { stream.Close() })
	if err != nil {
		shutdown.Fatal(err)
	}
	go s.handleLeaderStream(leaders, hb.Addr())
	go s.startHTTPServer(os.Getenv("PORT"))

	if err := s.Run(); err != nil {
		shutdown.Fatal(err)
	}
	shutdown.Exit()
}

func (s *Scheduler) Run() error {
	log := s.log.New("fn", "Run")
	log.Info("starting scheduler loop")
	defer log.Info("exiting scheduler loop")

	tickChannel(s.syncJobs, 30*time.Second)
	tickChannel(s.syncFormations, time.Minute)

	stream, err := s.StreamHostEvents(s.hostChange)
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

	err = nil
	for {
		if err != nil {
			log.Error("An error occurred", "error", err)
			err = nil
		}
		select {
		case <-s.stop:
			close(s.putJobs)
			return nil
		case isLeader := <-s.changeLeader:
			s.HandleLeaderChange(isLeader)
			continue
		default:
		}

		// Handle events that reconcile scheduler state with the cluster
		select {
		case req := <-s.jobRequests:
			err = s.HandleJobRequest(req)
			continue
		case e := <-s.hostChange:
			err = s.HandleHostChange(e)
			continue
		case he := <-s.hostEvents:
			err = s.HandleHostEvent(he)
			continue
		case ef := <-s.formationChange:
			err = s.FormationChange(ef)
			continue
		default:
		}

		// Handle sync events
		select {
		case <-s.syncFormations:
			err = s.SyncFormations()
			continue
		case <-s.syncJobs:
			err = s.SyncJobs()
			continue
		default:
		}

		// Finally, handle triggering cluster changes
		select {
		case <-s.rectifyJobs:
			err = s.RectifyJobs()
		case <-time.After(10 * time.Millisecond):
			// block so that
			//	1) mutate events are given a chance to happen
			//  2) we don't spin hot
		}
	}
	return nil
}

func (s *Scheduler) SyncJobs() (err error) {
	fLog := s.log.New("fn", "SyncJobs")

	defer func() {
		s.sendEvent(NewEvent(EventTypeClusterSync, err, nil))
	}()

	newSchedulerJobs := make(map[string]*Job)
	hosts, err := s.getHosts()
	if err != nil {
		return errors.New("Unable to query hosts")
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
		return errors.New("RectifyJobs(): this scheduler is not the leader")
	}

	log := s.log.New("fn", "RectifyJobs")
	defer func() {
		if err != nil {
			log.Error("error rectifying jobs", "err", err)
		}
		s.sendEvent(NewEvent(EventTypeRectifyJobs, err, nil))
	}()

	fj := NewPendingJobs(s.jobs)
	fj.Update(s.pendingStarts)
	fj.Update(s.pendingStops)

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

		if eq := reflect.DeepEqual(clusterProcs, schedulerProcs); (len(clusterProcs) != 0 || len(schedulerProcs) != 0) && !eq {
			log.Debug("Updating processes", "formation.processes", schedulerProcs, "cluster.processes", clusterProcs)
			schedulerFormation.Processes = clusterProcs

			diff := schedulerFormation.Update(schedulerProcs)
			s.sendDiffRequests(schedulerFormation, diff)
		}
	}

	for fKey, schedulerFormation := range s.formations {
		if _, ok := fj[fKey]; !ok && len(schedulerFormation.Processes) > 0 {
			log.Debug("Re-asserting processes", "formation.key", fKey, "formation.processes", schedulerFormation.Processes)
			s.sendDiffRequests(schedulerFormation, schedulerFormation.Processes)
		}
	}

	return err
}

func (s *Scheduler) FormationChange(ef *ct.ExpandedFormation) (err error) {
	defer func() {
		s.sendEvent(NewEvent(EventTypeFormationChange, err, nil))
	}()

	_, err = s.changeFormation(ef)
	if err != nil {
		return err
	}
	// Trigger sync jobs in case we've ignored an existing job because we
	// didn't know about the formation.
	triggerChan(s.syncJobs)

	return nil
}

func (s *Scheduler) HandleJobRequest(req *JobRequest) (err error) {
	// Ensure that we've cleared this request from pendingJobs
	if !s.isLeader {
		return errors.New("HandleJobRequest(req): this scheduler is not the leader")
	}

	log := s.log.New("fn", "HandleJobRequest")
	defer func() {
		if err != nil {
			log.Error("error handling job request", "err", err)
		}
		s.sendEvent(NewEvent(EventTypeJobRequest, err, req))
	}()

	switch req.RequestType {
	case JobRequestTypeUp:
		err = s.startJob(req)
	case JobRequestTypeDown:
		err = s.stopJob(req)
	default:
		err = fmt.Errorf("unknown job request type: %s", req.RequestType)
	}
	return err
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

func (s *Scheduler) HandleLeaderChange(isLeader bool) {
	s.isLeader = isLeader
	if isLeader {
		triggerChan(s.rectifyJobs)
	}
	s.sendEvent(NewEvent(EventTypeLeaderChange, nil, isLeader))
}

func (s *Scheduler) sendDiffRequests(f *Formation, diff map[string]int) {
	for typ, n := range diff {
		if n > 0 {
			for i := 0; i < n; i++ {
				req := NewJobRequest(f, JobRequestTypeUp, typ, "", "")
				s.pendingStarts.AddJob(req.Job)
				s.jobRequests <- req
			}
		} else if n < 0 {
			for i := 0; i < -n; i++ {
				req := NewJobRequest(f, JobRequestTypeDown, typ, "", "")
				s.pendingStops.RemoveJob(req.Job)
				s.jobRequests <- req
			}
		}
	}
}

func (s *Scheduler) followHost(h utils.HostClient) error {
	log := s.log.New("fn", "followHost")
	_, ok := s.hostStreams[h.ID()]
	if !ok {
		log.Info("Following host", "host.id", h.ID())
		hostEvents := make(chan *host.Event)
		stream, err := h.StreamEvents("all", hostEvents)
		if err == nil {
			s.hostStreams[h.ID()] = stream
			triggerChan(s.syncFormations)

			go func() {
				for {
					e, ok := <-hostEvents
					if !ok {
						return
					}
					s.hostEvents <- e
				}
			}()
		} else {
			return fmt.Errorf("Error following host with id %q", h.ID())
		}
	} else {
		return fmt.Errorf("Already following host with id %q", h.ID())
	}
	return nil
}

func (s *Scheduler) unfollowHost(id string) error {
	log := s.log.New("fn", "unfollowHost")
	stream, ok := s.hostStreams[id]
	if ok {
		log.Info("Unfollowing host", "host.id", id)
		for jobID, job := range s.jobs {
			if job.HostID == id {
				delete(s.jobs, jobID)
			}
		}
		stream.Close()
		delete(s.hostStreams, id)
		triggerChan(s.syncFormations)
		return nil
	} else {
		return fmt.Errorf("Not currently following host with ID %q", id)
	}
}

func (s *Scheduler) HandleHostChange(e *discoverd.Event) (err error) {
	defer func() {
		s.sendEvent(NewEvent(EventTypeHostChange, err, nil))
	}()
	if e == nil || e.Instance == nil || e.Instance.Meta == nil {
		return fmt.Errorf("Invalid data in host change event: %v", e)
	}
	hostID, ok := e.Instance.Meta["id"]
	if !ok {
		return fmt.Errorf("No hostID specified in host change event: %v", e)
	}
	switch e.Kind {
	case discoverd.EventKindUp:
		h, err := s.Host(hostID)
		if err != nil {
			return err
		}
		return s.followHost(h)
	case discoverd.EventKindDown:
		return s.unfollowHost(hostID)
	}
	return nil
}

func (s *Scheduler) HandleHostEvent(he *host.Event) (err error) {
	log := s.log.New("fn", "HandleHostEvent")

	defer func() {
		if err != nil {
			log.Error("error handling host event", "err", err)
		}
	}()

	log.Info("Handling job event", "job.event.type", he.Event)
	job, err := s.handleActiveJob(he.Job)

	switch he.Event {
	case host.JobEventStart:
		s.sendEvent(NewEvent(EventTypeJobStart, err, job))
	case host.JobEventStop:
		s.sendEvent(NewEvent(EventTypeJobStop, err, nil))
	}

	triggerChan(s.rectifyJobs)

	return err
}

func (s *Scheduler) handleActiveJob(activeJob *host.ActiveJob) (j *Job, err error) {
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
	return j, nil
}

func (s *Scheduler) changeFormation(ef *ct.ExpandedFormation) (*Formation, error) {
	if ef.App == nil || ef.Release == nil {
		return nil, fmt.Errorf("Formation given without app (%v), or release (%v)", ef.App, ef.Release)
	}
	s.expandOmni(ef)
	f := s.formations.Get(ef.App.ID, ef.Release.ID)
	if f == nil {
		f = s.formations.Add(NewFormation(ef))
	} else {
		f.Processes = ef.Processes
	}
	return f, nil
}

func (s *Scheduler) updateFormation(controllerFormation *ct.Formation) (*Formation, error) {
	ef, err := utils.ExpandedFormationFromFormation(s, controllerFormation)
	if err != nil {
		return nil, err
	}
	return s.changeFormation(ef)
}

func (s *Scheduler) startJob(req *JobRequest) (err error) {
	log := s.log.New("fn", "startJob")
	log.Info("starting job", "job.type", req.Type, "job.startedAt", req.startedAt, "job.restarts", req.restarts)
	defer func() {
		if err != nil {
			s.pendingStarts.RemoveJob(req.Job)
			s.scheduleJobRequest(req)
			log.Error("error starting job", "err", err)
		}
	}()

	host, err := s.findBestHost(req.Formation, req.Type, req.HostID)
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
	s.pendingStarts.RemoveJob(req.Job)
	req.HostID = host.ID()
	s.pendingStarts.AddJob(req.Job)
	log.Info("requested job start from host", "host.id", host.ID(), "job.type", req.Type, "job.id", config.ID)
	return nil
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
		// TODO this is a terrible solution. We need to actually store the jobs that are pending stops.
		// Proposed solution: create s.stopped and use that to track stopped jobs. Treat a job as stopped
		// as soon as the request is executed.
		formationKey := utils.FormationKey{AppID: req.AppID, ReleaseID: req.ReleaseID}
		formJobs := NewFormationJobs(s.jobs)
		formJob := formJobs[formationKey]
		typJobs := jobsByStartTime(formJob[req.Type])
		if len(typJobs) == 0 {
			return fmt.Errorf("No running jobs of type %q", req.Type)
		}
		sort.Sort(typJobs)

		typProcs := -s.pendingStops[formationKey][req.Type][""]
		if typProcs > 0 && typProcs <= len(typJobs) {
			job = typJobs[typProcs-1]
		} else {
			return fmt.Errorf("Unable to stop the job; there are more stops pending than there are jobs. Job count: %v, pending stops: %v", len(typJobs), typProcs)
		}
	} else {
		var ok bool
		job, ok = s.jobs[req.JobID]
		if !ok {
			return fmt.Errorf("No running job with ID %q", req.JobID)
		}
	}
	host, err := s.Host(job.HostID)
	if err != nil {
		s.unfollowHost(job.HostID)
		return err
	}
	if err := host.StopJob(job.JobID); err != nil {
		return err
	}
	s.pendingStops.AddJob(req.Job)
	req.HostID = host.ID()
	s.pendingStops.RemoveJob(req.Job)
	log.Info("requested job stop from host", "host.id", host.ID(), "job.type", req.Type, "job.id", job.JobID)
	return nil
}

func jobConfig(req *JobRequest, hostID string) *host.Job {
	return utils.JobConfig(req.Job.Formation.ExpandedFormation, req.Type, hostID)
}

func (s *Scheduler) findBestHost(formation *Formation, typ, hostID string) (utils.HostClient, error) {
	hosts, err := s.getHosts()
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, errors.New("no hosts found")
	}

	if hostID == "" {
		fj := NewPendingJobs(s.jobs)
		fj.Update(s.pendingStarts)
		fj.Update(s.pendingStops)
		counts := fj.GetHostJobCounts(formation.key(), typ)
		var minCount int = math.MaxInt32
		for _, host := range hosts {
			count, ok := counts[host.ID()]
			if !ok || count < minCount {
				minCount = count
				hostID = host.ID()
			}
		}
		s.log.Info("Finding best host.", "host", hostID, "counts", counts)
	}
	return s.Host(hostID)
}

func (s *Scheduler) getHosts() ([]utils.HostClient, error) {
	hosts, err := s.Hosts()

	if err != nil {
		return nil, err
	}

	// Ensure that we're only following hosts that we can discover
	knownHosts := make(map[string]struct{})
	for id := range s.hostStreams {
		knownHosts[id] = struct{}{}
	}
	for _, h := range hosts {
		delete(knownHosts, h.ID())
		s.followHost(h)
	}
	for id := range knownHosts {
		s.unfollowHost(id)
	}
	return hosts, nil
}

func (s *Scheduler) Stop() error {
	s.log.Info("stopping scheduler loop", "fn", "Stop")
	s.stopOnce.Do(func() {
		close(s.stop)
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
		s.handleJobStart(job)
		controllerState = "up"
	default:
		s.handleJobStop(job)
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
		if pt.Omni && ef.Processes != nil && ef.Processes[typ] > 0 {
			ef.Processes[typ] *= len(s.hostStreams)
		}
	}
}

func (s *Scheduler) scheduleJobRequest(req *JobRequest) {
	backoff := s.getBackoffDuration(req.restarts)
	req.startedAt = time.Now()
	req.restarts += 1
	s.pendingStarts.AddJob(req.Job)
	time.AfterFunc(backoff, func() {
		s.jobRequests <- req
	})
}

func (s *Scheduler) getBackoffDuration(restarts uint) time.Duration {
	// Overflow guard
	if restarts > 30 {
		return s.backoffPeriod
	}
	delay := time.Duration(1<<restarts) * time.Second

	if delay > s.backoffPeriod {
		return s.backoffPeriod
	}
	return delay
}

func (s *Scheduler) handleJobStart(job *Job) {
	_, ok := s.jobs[job.JobID]
	if s.isLeader && !ok && s.pendingStarts.HasStarts(job) {
		s.pendingStarts.RemoveJob(job)
	}
	s.jobs[job.JobID] = job
}

func (s *Scheduler) handleJobStop(job *Job) {
	_, ok := s.jobs[job.JobID]
	if s.isLeader && ok && s.pendingStops.HasStops(job) {
		s.pendingStops.AddJob(job)
	}
	delete(s.jobs, job.JobID)
}

func (s *Scheduler) handleLeaderStream(leaders chan *discoverd.Instance, thisSchedulerAddr string) {
	for leader := range leaders {
		s.ChangeLeader(leader.Addr == thisSchedulerAddr)
	}
}

func (s *Scheduler) startHTTPServer(port string) {
	status.AddHandler(status.HealthyHandler)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		s.log.Error("An error occurred in the health check server", "error", err)
		s.Stop()
	}
}

func tickChannel(ch chan struct{}, d time.Duration) {
	ticker := time.Tick(d)

	go func() {
		for range ticker {
			triggerChan(ch)
		}
	}()
}

func triggerChan(ch chan struct{}) {
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
	s.log.Info("sending event to listeners", "event.type", event.Type(), "event.error", event.Err(), "listeners.count", len(s.listeners))
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
	EventTypeLeaderChange    EventType = "leader-change"
	EventTypeClusterSync     EventType = "cluster-sync"
	EventTypeFormationSync   EventType = "formation-sync"
	EventTypeFormationChange EventType = "formation-change"
	EventTypeRectifyJobs     EventType = "rectify-jobs"
	EventTypeJobStart        EventType = "start-job"
	EventTypeJobStop         EventType = "stop-job"
	EventTypeJobRequest      EventType = "request-job"
	EventTypeHostChange      EventType = "host-change"
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

type LeaderChangeEvent struct {
	Event
	IsLeader bool
}

type JobRequestEvent struct {
	Event
	Request *JobRequest
}

func NewEvent(typ EventType, err error, data interface{}) Event {
	switch typ {
	case EventTypeJobStart:
		job, _ := data.(*Job)
		return &JobStartEvent{Event: &DefaultEvent{err: err, typ: typ}, Job: job}
	case EventTypeJobRequest:
		req, _ := data.(*JobRequest)
		return &JobRequestEvent{Event: &DefaultEvent{err: err, typ: typ}, Request: req}
	case EventTypeLeaderChange:
		isLeader, _ := data.(bool)
		return &LeaderChangeEvent{Event: &DefaultEvent{err: err, typ: typ}, IsLeader: isLeader}
	default:
		return &DefaultEvent{err: err, typ: typ}
	}
}

func getBackoffPeriod() time.Duration {
	backoffPeriod := 10 * time.Minute

	if period := os.Getenv("BACKOFF_PERIOD"); period != "" {
		p, err := time.ParseDuration(period)
		if err == nil {
			backoffPeriod = p
		}
	}

	return backoffPeriod
}
