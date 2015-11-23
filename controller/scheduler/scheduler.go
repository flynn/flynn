package main

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	discoverd "github.com/flynn/flynn/discoverd/client"
	host "github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/stream"
)

const (
	eventBufferSize      = 1000
	maxJobAttempts       = 30
	jobAttemptInterval   = 500 * time.Millisecond
	defaultMaxHostChecks = 10
)

var (
	ErrNotLeader  = errors.New("scheduler is not the leader")
	ErrNoHosts    = errors.New("no hosts found")
	ErrJobStopped = errors.New("job has been marked as stopped")
)

var logger = log15.New("component", "scheduler")

type Scheduler struct {
	utils.ControllerClient
	utils.ClusterClient

	discoverd Discoverd
	isLeader  bool

	backoffPeriod time.Duration
	maxHostChecks int

	formations Formations
	hosts      map[string]*Host
	jobs       Jobs

	jobEvents chan *host.Event

	listeners map[chan Event]struct{}
	listenMtx sync.RWMutex

	stop     chan struct{}
	stopOnce sync.Once

	syncJobs          chan struct{}
	syncFormations    chan struct{}
	syncHosts         chan struct{}
	hostChecks        chan struct{}
	rectify           chan struct{}
	hostEvents        chan *discoverd.Event
	formationEvents   chan *ct.ExpandedFormation
	putJobs           chan *ct.Job
	placementRequests chan *PlacementRequest

	rectifyBatch map[utils.FormationKey]struct{}

	// formationlessJobs is a map of formation keys to a list of jobs
	// which are in-memory but do not have a formation (because the
	// formation lookup failed when we got an event for the job), and is
	// used to update the jobs once we get the formation during a sync
	// so that we can determine if the job should actually be running
	formationlessJobs map[utils.FormationKey]map[string]*Job
}

func NewScheduler(cluster utils.ClusterClient, cc utils.ControllerClient, disc Discoverd) *Scheduler {
	return &Scheduler{
		ControllerClient:  cc,
		ClusterClient:     cluster,
		discoverd:         disc,
		backoffPeriod:     getBackoffPeriod(),
		maxHostChecks:     defaultMaxHostChecks,
		hosts:             make(map[string]*Host),
		jobs:              make(map[string]*Job),
		formations:        make(Formations),
		listeners:         make(map[chan Event]struct{}),
		jobEvents:         make(chan *host.Event, eventBufferSize),
		stop:              make(chan struct{}),
		syncJobs:          make(chan struct{}, 1),
		syncFormations:    make(chan struct{}, 1),
		syncHosts:         make(chan struct{}, 1),
		hostChecks:        make(chan struct{}, 1),
		rectifyBatch:      make(map[utils.FormationKey]struct{}),
		rectify:           make(chan struct{}, 1),
		formationEvents:   make(chan *ct.ExpandedFormation, eventBufferSize),
		hostEvents:        make(chan *discoverd.Event, eventBufferSize),
		putJobs:           make(chan *ct.Job, eventBufferSize),
		placementRequests: make(chan *PlacementRequest, eventBufferSize),
		formationlessJobs: make(map[utils.FormationKey]map[string]*Job),
	}
}

func main() {
	logger.SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StdoutHandler))
	log := logger.New("fn", "main")

	log.Info("creating cluster and controller clients")
	hc := &http.Client{Timeout: 5 * time.Second}
	clusterClient := utils.ClusterClientWrapper(cluster.NewClientWithHTTP(nil, hc))
	controllerClient, err := controller.NewClient("", os.Getenv("AUTH_KEY"))
	if err != nil {
		log.Error("error creating controller client", "err", err)
		shutdown.Fatal(err)
	}
	s := NewScheduler(clusterClient, controllerClient, newDiscoverdWrapper())
	log.Info("started scheduler", "backoffPeriod", s.backoffPeriod)

	go s.startHTTPServer(os.Getenv("PORT"))

	if err := s.Run(); err != nil {
		shutdown.Fatal(err)
	}
	shutdown.Exit()
}

func (s *Scheduler) streamFormationEvents() error {
	log := logger.New("fn", "streamFormationEvents")

	var events chan *ct.ExpandedFormation
	var stream stream.Stream
	var since *time.Time
	connect := func() (err error) {
		log.Info("connecting formation event stream")
		events = make(chan *ct.ExpandedFormation, eventBufferSize)
		stream, err = s.StreamFormations(since, events)
		if err != nil {
			log.Error("error connecting formation event stream", "err", err)
		}
		return
	}
	strategy := attempt.Strategy{Delay: 100 * time.Millisecond, Total: time.Minute}
	if err := strategy.Run(connect); err != nil {
		return err
	}

	current := make(chan struct{})
	go func() {
		var isCurrent bool
	outer:
		for {
			for formation := range events {
				// an empty formation indicates we now have the
				// current list of formations.
				if formation.App == nil {
					if !isCurrent {
						isCurrent = true
						close(current)
					}
					continue
				}
				since = &formation.UpdatedAt
				// if we are not current, explicitly handle the event
				// so that the scheduler has the current list of
				// formations before starting the main loop.
				if !isCurrent {
					s.HandleFormationChange(formation)
					continue
				}
				s.formationEvents <- formation
			}
			log.Warn("formation event stream disconnected", "err", stream.Err())
			for {
				if err := connect(); err == nil {
					continue outer
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	select {
	case <-current:
		return nil
	case <-time.After(30 * time.Second):
		return errors.New("timed out waiting for current formation list")
	}
}

func (s *Scheduler) streamHostEvents() error {
	log := logger.New("fn", "streamHostEvents")

	var events chan *discoverd.Event
	var stream stream.Stream
	connect := func() (err error) {
		log.Info("connecting host event stream")
		events = make(chan *discoverd.Event, eventBufferSize)
		stream, err = s.StreamHostEvents(events)
		if err != nil {
			log.Error("error connecting host event stream", "err", err)
		}
		return
	}
	if err := connect(); err != nil {
		return err
	}

	current := make(chan struct{})
	go func() {
		var isCurrent bool
	outer:
		for {
			for event := range events {
				switch event.Kind {
				case discoverd.EventKindCurrent:
					if !isCurrent {
						isCurrent = true
						close(current)
					}
				case discoverd.EventKindUp, discoverd.EventKindDown:
					// if we are not current, explicitly handle the event
					// so that the scheduler is streaming job events from
					// all current hosts before starting the main loop.
					if !isCurrent {
						s.HandleHostEvent(event)
						continue
					}
					s.hostEvents <- event
				}
			}
			log.Warn("host event stream disconnected", "err", stream.Err())
			for {
				if err := connect(); err == nil {
					continue outer
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	select {
	case <-current:
		return nil
	case <-time.After(30 * time.Second):
		return errors.New("timed out waiting for current host list")
	}
}

func (s *Scheduler) Run() error {
	log := logger.New("fn", "Run")
	log.Info("starting scheduler loop")
	defer log.Info("scheduler loop exited")

	// stream host events (which will start watching job events on
	// all current hosts before returning) *before* registering in
	// service discovery so that there is always at least one scheduler
	// watching all job events, even during a deployment.
	if err := s.streamHostEvents(); err != nil {
		return err
	}

	isLeader, err := s.discoverd.Register()
	if err != nil {
		return err
	}
	s.HandleLeaderChange(isLeader)
	leaderCh := s.discoverd.LeaderCh()

	if err := s.streamFormationEvents(); err != nil {
		return err
	}

	s.tickSyncJobs(30 * time.Second)
	s.tickSyncFormations(time.Minute)
	s.tickSyncHosts(10 * time.Second)

	go s.RunPutJobs()

	for {
		select {
		case <-s.stop:
			log.Info("stopping scheduler loop")
			close(s.putJobs)
			return nil
		case isLeader := <-leaderCh:
			s.HandleLeaderChange(isLeader)
			continue
		default:
		}

		// Handle events that reconcile scheduler state with the cluster
		select {
		case req := <-s.placementRequests:
			s.HandlePlacementRequest(req)
			continue
		case e := <-s.hostEvents:
			s.HandleHostEvent(e)
			continue
		case <-s.hostChecks:
			s.PerformHostChecks()
			continue
		case e := <-s.jobEvents:
			s.HandleJobEvent(e)
			continue
		case f := <-s.formationEvents:
			s.HandleFormationChange(f)
			continue
		default:
		}

		// Handle sync events
		select {
		case <-s.syncFormations:
			s.SyncFormations()
			continue
		case <-s.syncJobs:
			s.SyncJobs()
			continue
		case <-s.syncHosts:
			s.SyncHosts()
			continue
		default:
		}

		// Finally, handle triggering cluster changes.
		// Re-select on all the channels so we don't have to sleep nor spin
		select {
		case <-s.rectify:
			s.HandleRectify()
		case <-s.stop:
			log.Info("stopping scheduler loop")
			close(s.putJobs)
			return nil
		case isLeader := <-leaderCh:
			s.HandleLeaderChange(isLeader)
		case req := <-s.placementRequests:
			s.HandlePlacementRequest(req)
		case e := <-s.hostEvents:
			s.HandleHostEvent(e)
		case <-s.hostChecks:
			s.PerformHostChecks()
		case e := <-s.jobEvents:
			s.HandleJobEvent(e)
		case f := <-s.formationEvents:
			s.HandleFormationChange(f)
		case <-s.syncFormations:
			s.SyncFormations()
		case <-s.syncJobs:
			s.SyncJobs()
		case <-s.syncHosts:
			s.SyncHosts()
		}
	}
	return nil
}

func (s *Scheduler) SyncJobs() (err error) {
	defer s.sendEvent(EventTypeClusterSync, nil, nil)

	log := logger.New("fn", "SyncJobs")
	log.Info("syncing jobs")

	defer func() {
		if err != nil {
			// try again soon
			time.AfterFunc(100*time.Millisecond, s.triggerSyncJobs)
		}
	}()

	for id, host := range s.hosts {
		jobs, err := host.client.ListJobs()
		if err != nil {
			log.Error("error getting host jobs", "host.id", id, "err", err)
			return err
		}

		for _, job := range jobs {
			s.handleActiveJob(&job)
		}
	}

	return nil
}

func (s *Scheduler) SyncFormations() {
	defer s.sendEvent(EventTypeFormationSync, nil, nil)

	log := logger.New("fn", "SyncFormations")
	log.Info("syncing formations")

	formations, err := s.FormationListActive()
	if err != nil {
		log.Error("error getting active formations", "err", err)
		return
	}

	active := make(map[utils.FormationKey]struct{}, len(formations))
	for _, f := range formations {
		active[utils.FormationKey{AppID: f.App.ID, ReleaseID: f.Release.ID}] = struct{}{}
		s.handleFormation(f)
	}

	// check that all formations we think are active are still active
	for _, f := range s.formations {
		if _, ok := active[f.key()]; !ok && !f.GetProcesses().IsEmpty() {
			log.Warn("formation should not be active, scaling down", "app.id", f.App.ID, "release.id", f.Release.ID)
			f.Processes = nil
			s.triggerRectify(f.key())
		}
	}
}

func (s *Scheduler) SyncHosts() (err error) {
	log := logger.New("fn", "SyncHosts")
	log.Info("syncing hosts")

	defer func() {
		if err != nil {
			// try again soon
			time.AfterFunc(100*time.Millisecond, s.triggerSyncHosts)
		}
	}()

	hosts, err := s.Hosts()
	if err != nil {
		log.Error("error getting hosts", "err", err)
		return err
	}

	known := make(map[string]struct{})
	var followErr error
	for _, host := range hosts {
		known[host.ID()] = struct{}{}

		if err := s.followHost(host); err != nil {
			log.Error("error following host", "host.id", host.ID(), "err", err)
			// finish the sync before returning the error
			followErr = err
		}
	}

	// mark any hosts as unhealthy which are not returned from s.Hosts()
	for id, host := range s.hosts {
		if _, ok := known[id]; !ok {
			s.markHostAsUnhealthy(host)
		}
	}

	if followErr != nil {
		return followErr
	}

	// return an error to trigger another sync if no hosts were found
	if len(hosts) == 0 {
		log.Error(ErrNoHosts.Error())
		return ErrNoHosts
	}

	return nil
}

func (s *Scheduler) HandleRectify() error {
	for key := range s.rectifyBatch {
		s.RectifyFormation(key)
	}
	s.rectifyBatch = make(map[utils.FormationKey]struct{})
	return nil
}

func (s *Scheduler) RectifyFormation(key utils.FormationKey) {
	if !s.isLeader {
		return
	}
	defer s.sendEvent(EventTypeRectify, nil, key)

	formation := s.formations[key]
	diff := s.formationDiff(formation)
	if diff.IsEmpty() {
		return
	}
	s.handleFormationDiff(formation, diff)
}

func (s *Scheduler) formationDiff(formation *Formation) Processes {
	if formation == nil {
		return nil
	}
	key := formation.key()
	expected := formation.GetProcesses()
	actual := s.jobs.GetProcesses(key)
	if expected.Equals(actual) {
		return nil
	}
	formation.Processes = actual
	diff := formation.Update(expected)
	log := logger.New("fn", "formationDiff", "app.id", key.AppID, "release.id", key.ReleaseID)
	log.Info("expected different from actual", "expected", expected, "actual", actual, "diff", diff)
	return diff
}

func (s *Scheduler) HandleFormationChange(ef *ct.ExpandedFormation) {
	var err error
	defer func() {
		s.sendEvent(EventTypeFormationChange, err, nil)
	}()

	log := logger.New("fn", "HandleFormationChange", "app.id", ef.App.ID, "release.id", ef.Release.ID, "processes", ef.Processes)
	log.Info("handling formation change")
	s.handleFormation(ef)
}

func (s *Scheduler) HandlePlacementRequest(req *PlacementRequest) {
	if !s.isLeader {
		req.Error(ErrNotLeader)
		return
	}

	// don't attempt to place a job which has been marked as stopped
	if req.Job.state == JobStateStopped {
		req.Error(ErrJobStopped)
		return
	}

	log := logger.New("fn", "HandlePlacementRequest", "app.id", req.Job.AppID, "release.id", req.Job.ReleaseID, "job.type", req.Job.Type)
	log.Info("handling placement request")

	if len(s.hosts) == 0 {
		req.Error(ErrNoHosts)
		return
	}

	formation := req.Job.Formation
	counts := s.jobs.GetHostJobCounts(formation.key(), req.Job.Type)
	var minCount int = math.MaxInt32
	for id, h := range s.hosts {
		count, ok := counts[id]
		if !ok || count < minCount {
			minCount = count
			req.Host = h
		}
	}
	if req.Host == nil {
		req.Error(fmt.Errorf("unable to find a host out of %d hosts", len(s.hosts)))
		return
	}
	log.Info(fmt.Sprintf("placed job on host with least %s jobs", req.Job.Type), "host.id", req.Host.ID)

	req.Config = jobConfig(req.Job, req.Host.ID)
	req.Job.JobID = req.Config.ID
	req.Job.HostID = req.Host.ID
	req.Error(nil)
}

func (s *Scheduler) RunPutJobs() {
	log := logger.New("fn", "RunPutJobs")
	log.Info("starting job persistence loop")
	strategy := attempt.Strategy{Delay: 100 * time.Millisecond, Total: time.Minute}
	for job := range s.putJobs {
		err := strategy.RunWithValidator(func() error {
			return s.PutJob(job)
		}, httphelper.IsRetryableError)
		if err != nil {
			log.Error("error persisting job", "job.id", job.ID, "job.state", job.State, "err", err)
		}
	}
	log.Info("stopping job persistence loop")
}

func (s *Scheduler) HandleLeaderChange(isLeader bool) {
	log := logger.New("fn", "HandleLeaderChange")
	s.isLeader = isLeader
	if isLeader {
		log.Info("handling leader promotion")
		s.rectifyAll()
	} else {
		log.Info("handling leader demotion")
	}
	s.sendEvent(EventTypeLeaderChange, nil, isLeader)
}

func (s *Scheduler) handleFormationDiff(f *Formation, diff Processes) {
	log := logger.New("fn", "handleFormationDiff", "app.id", f.App.ID, "release.id", f.Release.ID)
	log.Info("formation in incorrect state", "diff", diff)
	for typ, n := range diff {
		if n > 0 {
			log.Info(fmt.Sprintf("starting %d new %s jobs", n, typ))
			for i := 0; i < n; i++ {
				job := &Job{
					InternalID: random.UUID(),
					Type:       typ,
					AppID:      f.App.ID,
					ReleaseID:  f.Release.ID,
					Formation:  f,
					startedAt:  time.Now(),
					state:      JobStateNew,
				}
				s.jobs.Add(job)
				go s.StartJob(job)
			}
		} else if n < 0 {
			log.Info(fmt.Sprintf("stopping %d %s jobs", -n, typ))
			for i := 0; i < -n; i++ {
				s.stopJob(f, typ)
			}
		}
	}
}

func (s *Scheduler) StartJob(job *Job) {
	log := logger.New("fn", "StartJob", "app.id", job.AppID, "release.id", job.ReleaseID, "job.type", job.Type)
	log.Info("starting job")

	for attempt := 0; attempt < maxJobAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(jobAttemptInterval)
		}
		log.Info("placing job in the cluster")
		config, host, err := s.PlaceJob(job)
		if err == ErrNotLeader {
			log.Warn("not starting job as not leader")
			return
		} else if err != nil {
			log.Error("error placing job in the cluster", "err", err)
			continue
		}

		if job.needsVolume() {
			log.Info("provisioning data volume", "host.id", host.ID)
			if err := utils.ProvisionVolume(host.client, config); err != nil {
				log.Error("error provisioning volume", "err", err)
				continue
			}
		}

		log.Info("adding job to the cluster", "host.id", host.ID, "job.id", config.ID)
		if err := host.client.AddJob(config); err != nil {
			log.Error("error adding job to the cluster", "err", err)
			continue
		}
		return
	}
	log.Error(fmt.Sprintf("error starting job after %d attempts", maxJobAttempts))
}

// PlacementRequest is sent from a StartJob goroutine to the main scheduler
// loop to place the job in the cluster (i.e. select a host and generate config
// for the job)
type PlacementRequest struct {
	Job    *Job
	Config *host.Job
	Host   *Host
	Err    chan error
}

func (r *PlacementRequest) Error(err error) {
	r.Err <- err
}

func (s *Scheduler) PlaceJob(job *Job) (*host.Job, *Host, error) {
	req := &PlacementRequest{
		Job: job,
		Err: make(chan error),
	}
	s.placementRequests <- req
	return req.Config, req.Host, <-req.Err
}

func (s *Scheduler) followHost(h utils.HostClient) error {
	if _, ok := s.hosts[h.ID()]; ok {
		return nil
	}

	host := NewHost(h)
	jobs, err := host.StreamEventsTo(s.jobEvents)
	if err != nil {
		return err
	}
	s.hosts[host.ID] = host

	for _, job := range jobs {
		s.handleActiveJob(&job)
	}

	s.triggerSyncFormations()

	return nil
}

func (s *Scheduler) unfollowHost(host *Host) {
	log := logger.New("fn", "unfollowHost", "host.id", host.ID)
	log.Info("unfollowing host")
	for _, job := range s.jobs {
		if job.HostID == host.ID {
			log.Info("removing job", "job.id", job.JobID)
			s.markAsStopped(job)
		}
	}

	log.Info("closing job event stream")
	host.Close()
	delete(s.hosts, host.ID)

	s.triggerSyncFormations()
}

func (s *Scheduler) markHostAsUnhealthy(host *Host) {
	logger.Warn("host service is down, marking as unhealthy and triggering host checks", "host.id", host.ID)
	host.healthy = false
	s.triggerHostChecks()
}

func (s *Scheduler) HandleHostEvent(e *discoverd.Event) {
	log := logger.New("fn", "HandleHostEvent", "event.type", e.Kind)
	log.Info("handling host event")

	var err error
	defer func() {
		s.sendEvent(EventTypeHostEvent, err, nil)
	}()

	switch e.Kind {
	case discoverd.EventKindUp:
		log = log.New("host.id", e.Instance.Meta["id"])
		log.Info("host is up, starting job event stream")
		var h utils.HostClient
		h, err = s.Host(e.Instance.Meta["id"])
		if err != nil {
			log.Error("error creating host client", "err", err)
			return
		}
		s.followHost(h)
	case discoverd.EventKindDown:
		id := e.Instance.Meta["id"]
		log = log.New("host.id", id)
		host, ok := s.hosts[id]
		if !ok {
			log.Warn("ignoring host down event, unknown host")
			return
		}
		s.markHostAsUnhealthy(host)
	}
}

func (s *Scheduler) PerformHostChecks() {
	log := logger.New("fn", "PerformHostChecks")
	log.Info("performing host checks")

	allHealthy := true

	for id, host := range s.hosts {
		if host.healthy {
			continue
		}

		log := log.New("host.id", id)
		log.Info("getting status of unhealthy host")
		if _, err := host.client.GetStatus(); err == nil {
			// assume the host is healthy if we can get its status
			log.Info("host is now healthy")
			host.healthy = true
			host.checks = 0
			continue
		}

		host.checks++
		if host.checks >= s.maxHostChecks {
			log.Warn(fmt.Sprintf("host unhealthy for %d consecutive checks, unfollowing", s.maxHostChecks))
			s.unfollowHost(host)
			continue
		}

		allHealthy = false
	}

	if !allHealthy {
		time.AfterFunc(time.Second, s.triggerHostChecks)
	}
}

func (s *Scheduler) HandleJobEvent(e *host.Event) {
	log := logger.New("fn", "HandleJobEvent", "job.id", e.JobID, "event.type", e.Event)

	log.Info("handling job event")
	job := s.handleActiveJob(e.Job)
	switch e.Event {
	case host.JobEventStart:
		s.sendEvent(EventTypeJobStart, nil, job)
	case host.JobEventStop:
		s.sendEvent(EventTypeJobStop, nil, job)
	}
}

func (s *Scheduler) handleActiveJob(activeJob *host.ActiveJob) *Job {
	hostJob := activeJob.Job
	appID := hostJob.Metadata["flynn-controller.app"]
	releaseID := hostJob.Metadata["flynn-controller.release"]

	// if job has no app metadata, just ignore it
	if appID == "" || releaseID == "" {
		return nil
	}

	jobType := hostJob.Metadata["flynn-controller.type"]

	// lookup the job in memory using either the scheduler ID from the
	// metadata, or the JobID in the case the job was started by the
	// controller (so has no scheduler ID)
	id := hostJob.Metadata["flynn-controller.scheduler_id"]
	if id == "" {
		id = hostJob.ID
	}
	job, ok := s.jobs[id]
	if !ok {
		// this is the first time we have seen the job so
		// add it to s.jobs
		job = &Job{
			InternalID: hostJob.ID,
			Type:       jobType,
			AppID:      appID,
			ReleaseID:  releaseID,
			HostID:     activeJob.HostID,
			JobID:      hostJob.ID,
			state:      JobStateNew,
		}
		s.jobs.Add(job)
	}

	job.startedAt = activeJob.StartedAt
	job.metadata = hostJob.Metadata

	s.handleJobStatus(job, activeJob.Status)

	return job
}

func (s *Scheduler) markAsStopped(job *Job) {
	s.handleJobStatus(job, host.StatusDone)
}

func (s *Scheduler) handleJobStatus(job *Job, status host.JobStatus) {
	log := logger.New("fn", "handleJobStatus", "job.id", job.JobID, "app.id", job.AppID, "release.id", job.ReleaseID, "job.type", job.Type)

	// update the job's state, keeping a reference to the previous state
	previousState := job.state
	switch status {
	case host.StatusStarting:
		job.state = JobStateStarting
	case host.StatusRunning:
		job.state = JobStateRunning
	case host.StatusDone, host.StatusCrashed, host.StatusFailed:
		job.state = JobStateStopped
	}

	// if the job's state has changed, persist it to the controller
	if job.state != previousState {
		log.Info("handling job status change", "from", previousState, "to", job.state)
		s.putJobs <- controllerJobFromSchedulerJob(
			job,
			jobState(status),
		)
	}

	// ensure the job has a known formation
	if job.Formation == nil {
		formation := s.formations.Get(job.AppID, job.ReleaseID)
		if formation == nil {
			ef, err := s.GetExpandedFormation(job.AppID, job.ReleaseID)
			if err != nil {
				// if we can't find the formation, track it as a formation-less
				// job so that it will be handled if we find the formation from
				// a future sync
				key := utils.FormationKey{AppID: job.AppID, ReleaseID: job.ReleaseID}
				jobs, ok := s.formationlessJobs[key]
				if !ok {
					jobs = make(map[string]*Job)
					s.formationlessJobs[key] = jobs
				}
				jobs[job.InternalID] = job
				log.Error("error looking up formation for job", "err", err)
				return
			}
			formation = s.handleFormation(ef)
		}
		job.Formation = formation
	}

	// if the job has no type, or has a type which is not part of the
	// release (e.g. a slugbuilder job), then we are done
	if job.Type == "" || !job.HasTypeFromRelease() {
		return
	}

	// if we are not the leader, then we are done
	if !s.isLeader {
		return
	}

	// if the job has just transitioned to the stopped state, check if we
	// expect it to be running, and if we do, restart it
	if previousState != JobStateStopped && job.state == JobStateStopped {
		if diff := s.formationDiff(job.Formation); diff[job.Type] > 0 {
			s.restartJob(job)
		}
	}

	// trigger a rectify for the job's formation in case we have too many
	// jobs of the given type and we need to stop some
	s.triggerRectify(job.Formation.key())
}

func (s *Scheduler) handleFormation(ef *ct.ExpandedFormation) (formation *Formation) {
	log := logger.New("fn", "handleFormation", "app.id", ef.App.ID, "release.id", ef.Release.ID)

	defer func() {
		// update any formation-less jobs
		if jobs, ok := s.formationlessJobs[formation.key()]; ok {
			for _, job := range jobs {
				job.Formation = formation
			}
			s.triggerRectify(formation.key())
			delete(s.formationlessJobs, formation.key())
		}
	}()

	for typ, proc := range ef.Release.Processes {
		if proc.Omni && ef.Processes != nil && ef.Processes[typ] > 0 {
			ef.Processes[typ] *= len(s.hosts)
		}
	}

	formation = s.formations.Get(ef.App.ID, ef.Release.ID)
	if formation == nil {
		log.Info("adding new formation", "processes", ef.Processes)
		formation = s.formations.Add(NewFormation(ef))
	} else {
		if formation.GetProcesses().Equals(ef.Processes) {
			return
		} else {
			log.Info("updating processes of existing formation", "processes", ef.Processes)
			formation.Processes = ef.Processes
		}
	}
	s.triggerRectify(formation.key())
	return
}

func (s *Scheduler) triggerRectify(key utils.FormationKey) {
	logger.Info("triggering rectify", "key", key)
	s.rectifyBatch[key] = struct{}{}
	select {
	case s.rectify <- struct{}{}:
	default:
	}
}

func (s *Scheduler) stopJob(f *Formation, typ string) (err error) {
	log := logger.New("fn", "stopJob", "job.type", typ)
	log.Info("stopping job")
	defer func() {
		if err != nil {
			log.Error("error stopping job", "err", err)
		}
	}()

	var runningJobs []*Job
	for _, job := range s.jobs.WithFormationAndType(f, typ) {
		switch job.state {
		case JobStateNew:
			// if it's a new job, we are in the process of starting
			// it, so just mark it as stopped (which will make the
			// StartJob goroutine fail the next time it tries to
			// place the job)
			log.Info("marking new job as stopped")
			job.state = JobStateStopped
			return nil
		case JobStateScheduled:
			// if the job is scheduled to be restarted, just cancel
			// the restart
			log.Info("stopping job which is scheduled to restart", "job.id", job.JobID)
			job.state = JobStateStopped
			job.restartTimer.Stop()
			return nil
		case JobStateStarting, JobStateRunning:
			runningJobs = append(runningJobs, job)
		}
	}
	if len(runningJobs) == 0 {
		return fmt.Errorf("no %s jobs running", typ)
	}

	// determine the most recent job
	job := runningJobs[0]
	for _, j := range runningJobs {
		if j.startedAt.After(job.startedAt) {
			job = j
		}
	}

	host, ok := s.hosts[job.HostID]
	if !ok {
		return fmt.Errorf("unknown host: %q", job.HostID)
	}

	log.Info("requesting host to stop job", "job.id", job.JobID, "host.id", job.HostID)
	job.state = JobStateStopping
	go func() {
		// host.StopJob can block, so run it in a goroutine
		if err := host.client.StopJob(job.JobID); err != nil {
			log.Error("error requesting host to stop job", "err", err)
		}
	}()
	return nil
}

func jobConfig(job *Job, hostID string) *host.Job {
	config := utils.JobConfig(job.Formation.ExpandedFormation, job.Type, hostID)
	config.Metadata["flynn-controller.scheduler_id"] = job.InternalID
	return config
}

func (s *Scheduler) Stop() error {
	log := logger.New("fn", "Stop")
	log.Info("stopping scheduler loop")
	s.stopOnce.Do(func() {
		close(s.stop)
	})
	return nil
}

func (s *Scheduler) Subscribe(events chan Event) stream.Stream {
	log := logger.New("fn", "Subscribe")
	log.Info("adding event subscriber")
	s.listenMtx.Lock()
	defer s.listenMtx.Unlock()
	s.listeners[events] = struct{}{}
	return &Stream{s, events}
}

func (s *Scheduler) Unsubscribe(events chan Event) {
	log := logger.New("fn", "Unsubscribe")
	log.Info("removing event subscriber")
	s.listenMtx.Lock()
	defer s.listenMtx.Unlock()
	delete(s.listeners, events)
}

func (s *Scheduler) Jobs() map[string]*Job {
	jobs := make(map[string]*Job, len(s.jobs))
	for id, j := range s.jobs {
		if j.IsRunning() {
			jobs[id] = j
		}
	}
	return jobs
}

func (s *Scheduler) restartJob(job *Job) {
	restarts := job.restarts
	// reset the restart count if it has been running for longer than the
	// back off period
	if job.startedAt.Before(time.Now().Add(-s.backoffPeriod)) {
		restarts = 0
	}
	backoff := s.getBackoffDuration(restarts)

	// create a new job so its state is tracked separately from the job
	// it is replacing
	newJob := &Job{
		InternalID: random.UUID(),
		Type:       job.Type,
		AppID:      job.AppID,
		ReleaseID:  job.ReleaseID,
		Formation:  job.Formation,
		startedAt:  time.Now(),
		state:      JobStateScheduled,
		restarts:   restarts + 1,
	}
	s.jobs.Add(newJob)

	logger.Info("scheduling job restart", "fn", "restartJob", "attempts", newJob.restarts, "delay", backoff)
	newJob.restartTimer = time.AfterFunc(backoff, func() { s.StartJob(newJob) })
}

func (s *Scheduler) getBackoffDuration(restarts uint) time.Duration {
	multiplier := 32 // max multiplier
	if restarts < 6 {
		// 2^(restarts - 1), or 0 if restarts == 0
		multiplier = (1 << restarts) >> 1
	}
	return s.backoffPeriod * time.Duration(multiplier)
}

func (s *Scheduler) startHTTPServer(port string) {
	log := logger.New("fn", "startHTTPServer")
	status.AddHandler(status.HealthyHandler)
	addr := ":" + port
	log.Info("serving HTTP requests", "addr", addr)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Error("error serving HTTP requests", "err", err)
		s.Stop()
	}
}

func (s *Scheduler) tickSyncJobs(d time.Duration) {
	logger.Info("starting sync jobs ticker", "duration", d)
	go func() {
		for range time.Tick(d) {
			s.triggerSyncJobs()
		}
	}()
}

func (s *Scheduler) tickSyncFormations(d time.Duration) {
	logger.Info("starting sync formations ticker", "duration", d)
	go func() {
		for range time.Tick(d) {
			s.triggerSyncFormations()
		}
	}()
}

func (s *Scheduler) tickSyncHosts(d time.Duration) {
	logger.Info("starting sync hosts ticker", "duration", d)
	go func() {
		for range time.Tick(d) {
			s.triggerSyncHosts()
		}
	}()
}

func (s *Scheduler) rectifyAll() {
	logger.Info("triggering rectify for all formations")
	for key := range s.formations {
		s.triggerRectify(key)
	}
}

func (s *Scheduler) triggerSyncJobs() {
	logger.Info("triggering job sync")
	select {
	case s.syncJobs <- struct{}{}:
	default:
	}
}

func (s *Scheduler) triggerSyncFormations() {
	logger.Info("triggering formation sync")
	select {
	case s.syncFormations <- struct{}{}:
	default:
	}
}

func (s *Scheduler) triggerSyncHosts() {
	logger.Info("triggering host sync")
	select {
	case s.syncHosts <- struct{}{}:
	default:
	}
}

func (s *Scheduler) triggerHostChecks() {
	logger.Info("triggering host checks")
	select {
	case s.hostChecks <- struct{}{}:
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

func (s *Scheduler) sendEvent(typ EventType, err error, data interface{}) {
	event := NewEvent(typ, err, data)
	s.listenMtx.RLock()
	defer s.listenMtx.RUnlock()
	if len(s.listeners) > 0 {
		logger.Info(fmt.Sprintf("sending %s event to %d listener(s)", event.Type(), len(s.listeners)), "event", event.Type(), "err", event.Err())
	}
	for ch := range s.listeners {
		// drop the event if the listener is too slow to avoid blocking the main loop
		select {
		case ch <- event:
		default:
		}
	}
}

type Event interface {
	Type() EventType
	Err() error
	Data() interface{}
}

type EventType string

const (
	EventTypeDefault         EventType = "default"
	EventTypeLeaderChange    EventType = "leader-change"
	EventTypeClusterSync     EventType = "cluster-sync"
	EventTypeFormationSync   EventType = "formation-sync"
	EventTypeFormationChange EventType = "formation-change"
	EventTypeRectify         EventType = "rectify"
	EventTypeJobStart        EventType = "start-job"
	EventTypeJobStop         EventType = "stop-job"
	EventTypeHostEvent       EventType = "host-event"
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

func (de *DefaultEvent) Data() interface{} {
	return nil
}

type JobEvent struct {
	Event
	Job *Job
}

func (je *JobEvent) Data() interface{} {
	return je.Job
}

type LeaderChangeEvent struct {
	Event
	IsLeader bool
}

func (lce *LeaderChangeEvent) Data() interface{} {
	return lce.IsLeader
}

type RectifyEvent struct {
	Event
	FormationKey utils.FormationKey
}

func (re *RectifyEvent) Data() interface{} {
	return re.FormationKey
}

func NewEvent(typ EventType, err error, data interface{}) Event {
	switch typ {
	case EventTypeJobStop:
		fallthrough
	case EventTypeJobStart:
		job, _ := data.(*Job)
		return &JobEvent{Event: &DefaultEvent{err: err, typ: typ}, Job: job}
	case EventTypeLeaderChange:
		isLeader, _ := data.(bool)
		return &LeaderChangeEvent{Event: &DefaultEvent{err: err, typ: typ}, IsLeader: isLeader}
	case EventTypeRectify:
		key, _ := data.(utils.FormationKey)
		return &RectifyEvent{Event: &DefaultEvent{err: err, typ: typ}, FormationKey: key}
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
