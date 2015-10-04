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
	eventBufferSize      int           = 1000
	maxJobAttempts       uint          = 30
	jobAttemptInterval   time.Duration = 500 * time.Millisecond
	defaultMaxHostChecks               = 10
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

	syncJobs        chan struct{}
	syncFormations  chan struct{}
	syncHosts       chan struct{}
	hostChecks      chan struct{}
	rectify         chan struct{}
	hostEvents      chan *discoverd.Event
	formationEvents chan *ct.ExpandedFormation
	jobRequests     chan *JobRequest
	putJobs         chan *ct.Job

	rectifyBatch map[utils.FormationKey]struct{}
}

func NewScheduler(cluster utils.ClusterClient, cc utils.ControllerClient, disc Discoverd) *Scheduler {
	return &Scheduler{
		ControllerClient: cc,
		ClusterClient:    cluster,
		discoverd:        disc,
		backoffPeriod:    getBackoffPeriod(),
		maxHostChecks:    defaultMaxHostChecks,
		hosts:            make(map[string]*Host),
		jobs:             make(map[string]*Job),
		formations:       make(Formations),
		listeners:        make(map[chan Event]struct{}),
		jobEvents:        make(chan *host.Event, eventBufferSize),
		stop:             make(chan struct{}),
		syncJobs:         make(chan struct{}, 1),
		syncFormations:   make(chan struct{}, 1),
		syncHosts:        make(chan struct{}, 1),
		hostChecks:       make(chan struct{}, 1),
		rectifyBatch:     make(map[utils.FormationKey]struct{}),
		rectify:          make(chan struct{}, 1),
		formationEvents:  make(chan *ct.ExpandedFormation, eventBufferSize),
		hostEvents:       make(chan *discoverd.Event, eventBufferSize),
		jobRequests:      make(chan *JobRequest, eventBufferSize),
		putJobs:          make(chan *ct.Job, eventBufferSize),
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
		case req := <-s.jobRequests:
			s.HandleJobRequest(req)
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
		case req := <-s.jobRequests:
			s.HandleJobRequest(req)
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

	knownJobs := make(Jobs)
	for id, host := range s.hosts {
		hostLog := log.New("host.id", id)

		hostLog.Info(fmt.Sprintf("getting jobs for host %s", id))
		activeJobs, err := host.client.ListJobs()
		if err != nil {
			hostLog.Error("error getting jobs list", "err", err)
			return err
		}
		hostLog.Info(fmt.Sprintf("got %d active job(s) for host %s", len(activeJobs), id))

		for _, job := range activeJobs {
			s.handleActiveJob(&job)
			if j, ok := s.jobs[job.Job.ID]; ok {
				knownJobs[j.JobID] = s.jobs[j.JobID]
			}
		}
	}

	for id, j := range s.jobs {
		if _, ok := knownJobs[id]; !ok && j.IsRunning() {
			s.jobs.SetState(j.JobID, JobStateStopped)
			if j.IsSchedulable() {
				s.triggerRectify(j.Formation.key())
			}
		}
	}

	return nil
}

func (s *Scheduler) SyncFormations() {
	defer s.sendEvent(EventTypeFormationSync, nil, nil)

	log := logger.New("fn", "SyncFormations")
	log.Info("syncing formations")

	log.Info("getting app list")
	apps, err := s.AppList()
	if err != nil {
		log.Error("error getting apps", "err", err)
		return
	}

	for _, app := range apps {
		appLog := log.New("app.id", app.ID)

		fs, err := s.FormationList(app.ID)
		if err != nil {
			appLog.Error("error getting formations", "err", err)
			continue
		}
		appLog.Debug(fmt.Sprintf("got %d formation(s) for %s app", len(fs), app.Name))

		for _, f := range fs {
			if _, err := s.handleControllerFormation(f); err != nil {
				appLog.Error("error handling controller formation", "release.id", f.ReleaseID, "err", err)
			}
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
		e := "no hosts found"
		log.Error(e)
		return errors.New(e)
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

func (s *Scheduler) HandleJobRequest(req *JobRequest) {
	log := logger.New("fn", "HandleJobRequest", "req.id", req.JobID)

	if !s.isLeader {
		log.Warn("ignoring job request as not service leader")
		return
	}

	log.Info("handling job request")
	err := s.startJob(req)
	if err != nil {
		log.Error("error handling job request", "err", err)
	}
	s.sendEvent(EventTypeJobRequest, err, req)
}

func (s *Scheduler) RunPutJobs() {
	log := logger.New("fn", "RunPutJobs")
	log.Info("starting job persistence loop")
	strategy := attempt.Strategy{Delay: 100 * time.Millisecond, Total: time.Minute}
	for job := range s.putJobs {
		jobLog := log.New("job.id", job.ID, "job.state", job.State)
		jobLog.Info("persisting job")
		err := strategy.RunWithValidator(func() error {
			return s.PutJob(job)
		}, httphelper.IsRetryableError)
		if err != nil {
			jobLog.Error("error persisting job", "err", err)
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
			log.Info(fmt.Sprintf("requesting %d new job(s) of type %s", n, typ))
			for i := 0; i < n; i++ {
				req := NewJobRequest(f, typ, "", random.UUID())
				req.state = JobStateRequesting
				s.jobs.AddJob(req.Job)
				s.HandleJobRequest(req)
			}
		} else if n < 0 {
			log.Info(fmt.Sprintf("requesting removal of %d job(s) of type %s", -n, typ))
			for i := 0; i < -n; i++ {
				s.stopJob(f, typ)
			}
		}
	}
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
	for id, job := range s.jobs {
		if job.HostID == host.ID {
			log.Info("removing job", "job.id", id)
			s.jobs.SetState(job.JobID, JobStateStopped)
			s.triggerRectify(job.Formation.key())
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
	job, err := s.handleActiveJob(e.Job)
	if err != nil {
		log.Error("error handling job event", "err", err)
	}

	switch e.Event {
	case host.JobEventStart:
		s.sendEvent(EventTypeJobStart, err, job)
	case host.JobEventStop:
		s.sendEvent(EventTypeJobStop, err, job)
	}
}

func (s *Scheduler) handleActiveJob(activeJob *host.ActiveJob) (*Job, error) {
	job := activeJob.Job
	appID := job.Metadata["flynn-controller.app"]
	appName := job.Metadata["flynn-controller.app_name"]
	releaseID := job.Metadata["flynn-controller.release"]
	jobType := job.Metadata["flynn-controller.type"]

	if appID == "" || releaseID == "" {
		return nil, errors.New("ignoring job due to lack of appID or releaseID")
	}
	log := logger.New("fn", "handleActiveJob", "job.id", job.ID, "app.id", appID, "release.id", releaseID, "job.type", jobType)

	var j *Job
	var err error
	j = s.jobs[job.ID]
	if j == nil {
		log.Info("creating new job")
		j = NewJob(nil, appID, releaseID, jobType, activeJob.HostID, job.ID)
	}
	if j.Formation == nil {
		log.Info("looking up formation")
		f := s.formations.Get(appID, releaseID)
		if f == nil {
			log.Info("job is from new formation, getting formation from controller")
			var cf *ct.Formation
			cf, err = s.GetFormation(appID, releaseID)
			if err != nil {
				log.Error("error getting formation", "err", err)
			} else {
				f, err = s.handleControllerFormation(cf)
				if err != nil {
					log.Error("error updating formation", "err", err)
				}
			}
		}
		j.Formation = f
	}
	j.startedAt = activeJob.StartedAt
	if s.SaveJob(j, appName, activeJob.Status, utils.JobMetaFromMetadata(job.Metadata)) {
		log.Info("saved job to controller")
	}

	return j, err
}

func (s *Scheduler) handleFormation(ef *ct.ExpandedFormation) *Formation {
	log := logger.New("fn", "handleFormation", "app.id", ef.App.ID, "release.id", ef.Release.ID)

	for typ, proc := range ef.Release.Processes {
		if proc.Omni && ef.Processes != nil && ef.Processes[typ] > 0 {
			ef.Processes[typ] *= len(s.hosts)
		}
	}

	f := s.formations.Get(ef.App.ID, ef.Release.ID)
	if f == nil {
		log.Info("adding new formation", "processes", ef.Processes)
		f = s.formations.Add(NewFormation(ef))
	} else {
		if f.GetProcesses().Equals(ef.Processes) {
			return f
		} else {
			log.Info("updating processes of existing formation", "processes", ef.Processes)
			f.Processes = ef.Processes
		}
	}
	s.triggerRectify(f.key())
	return f
}

func (s *Scheduler) triggerRectify(key utils.FormationKey) {
	logger.Info("triggering rectify", "key", key)
	s.rectifyBatch[key] = struct{}{}
	select {
	case s.rectify <- struct{}{}:
	default:
	}
}

func (s *Scheduler) handleControllerFormation(f *ct.Formation) (*Formation, error) {
	ef, err := utils.ExpandFormation(s, f)
	if err != nil {
		return nil, err
	}
	return s.handleFormation(ef), nil
}

func (s *Scheduler) startJob(req *JobRequest) (err error) {
	log := logger.New("fn", "startJob", "job.type", req.Type)
	log.Info("starting job", "job.restarts", req.restarts, "request.attempts", req.attempts)
	s.jobs.SetState(req.JobID, JobStateStopped)
	// We'll be changing the content of the job, including the job ID,
	// so we need to copy it to prevent it from getting stale in s.jobs
	newReq := req.Clone()
	newReq.HostID = ""
	newReq.JobID = random.UUID()
	newReq.state = JobStateRequesting
	defer func() {
		if err != nil {
			if newReq.attempts >= maxJobAttempts {
				log.Error("error starting job, max job attempts reached", "err", err)
			} else {
				log.Error("error starting job, trying again", "err", err)
				newReq.attempts++
				s.jobs[newReq.JobID] = newReq.Job
				time.AfterFunc(jobAttemptInterval, func() {
					s.jobRequests <- newReq
				})
			}
		} else {
			s.jobs[newReq.JobID] = newReq.Job
		}
	}()

	log.Info("determining best host for job")
	host, err := s.findBestHost(newReq.Formation, newReq.Type)
	if err != nil {
		log.Error("error determining best host for job", "err", err)
		return err
	}
	newReq.HostID = host.ID

	config := jobConfig(newReq, host.ID)
	newReq.JobID = config.ID

	// Provision a data volume on the host if needed.
	if newReq.needsVolume() {
		log.Info("provisioning volume")
		if err := utils.ProvisionVolume(host.client, config); err != nil {
			log.Error("error provisioning volume", "err", err)
			return err
		}
	}

	log.Info("requesting host to add job", "host.id", host.ID, "job.id", config.ID)
	if err := host.client.AddJob(config); err != nil {
		log.Error("error requesting host to add job", "err", err)
		return err
	}
	return nil
}

func (s *Scheduler) stopJob(f *Formation, typ string) (err error) {
	log := logger.New("fn", "stopJob", "job.type", typ)
	log.Info("stopping job")
	defer func() {
		if err != nil {
			log.Error("error stopping job", "err", err)
		}
	}()
	// TODO: stop job restart timers before attempting to stop a running job gh#1922

	typJobs := s.jobs.GetStoppableJobs(f.key(), typ)
	if len(typJobs) == 0 {
		e := fmt.Sprintf("no %s jobs running", typ)
		log.Error(e)
		return errors.New(e)
	}
	job := typJobs[0]
	for _, j := range typJobs {
		if j.startedAt.After(job.startedAt) {
			job = j
		}
	}

	log = log.New("job.id", job.JobID, "host.id", job.HostID)
	log.Info("selected job for termination")
	s.jobs.SetState(job.JobID, JobStateStopping)
	if job.HostID != "" {
		log = log.New("job.id", job.JobID, "host.id", job.HostID)
		host, ok := s.hosts[job.HostID]
		if !ok {
			e := "unable to stop job, unknown host"
			log.Error(e)
			return errors.New(e)
		}

		log.Info("requesting host to stop job")
		go func() {
			// host.StopJob can block, so run it in a goroutine
			if err := host.client.StopJob(job.JobID); err != nil {
				log.Error("error requesting host to stop job", "err", err)
			}
		}()
	}
	return nil
}

func jobConfig(req *JobRequest, hostID string) *host.Job {
	return utils.JobConfig(req.Job.Formation.ExpandedFormation, req.Type, hostID)
}

func (s *Scheduler) findBestHost(formation *Formation, typ string) (*Host, error) {
	log := logger.New("fn", "findBestHost", "app.id", formation.App.ID, "release.id", formation.Release.ID, "job.type", typ)

	if len(s.hosts) == 0 {
		e := "no hosts found"
		log.Error(e)
		return nil, errors.New(e)
	}

	counts := s.jobs.GetHostJobCounts(formation.key(), typ)
	var minCount int = math.MaxInt32
	var host *Host
	for id, h := range s.hosts {
		count, ok := counts[id]
		if !ok || count < minCount {
			minCount = count
			host = h
		}
	}
	if host == nil {
		return nil, fmt.Errorf("unable to find a host out of %d host(s)", len(s.hosts))
	}
	log.Info(fmt.Sprintf("using host with least %s jobs", typ), "host.id", host.ID)
	return host, nil
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

func (s *Scheduler) SaveJob(job *Job, appName string, status host.JobStatus, metadata map[string]string) bool {
	switch status {
	case host.StatusStarting:
		s.handleJobEvent(job, JobStateStarting)
	case host.StatusRunning:
		s.handleJobEvent(job, JobStateRunning)
	default:
		if job.IsStopped() || !job.IsSchedulable() {
			s.handleJobEvent(job, JobStateStopped)
		} else {
			diff := s.formationDiff(job.Formation)
			if diff[job.Type] < 0 {
				s.handleJobEvent(job, JobStateStopped)
			} else {
				// We want more jobs of this type, so this is a crash
				s.handleJobCrash(job)
			}
		}
	}
	if !s.jobs.IsJobInState(job.JobID, job.state) {
		// Only save the job to the controller if its state has changed
		log := logger.New("fn", "SaveJob", "job.id", job.JobID, "app.id", job.AppID, "app.name", appName, "release.id", job.ReleaseID, "job.type", job.Type, "job.status", status)
		log.Info("queuing job for persistence")
		s.putJobs <- controllerJobFromSchedulerJob(job, jobState(status), metadata)
		if job.IsSchedulable() {
			s.triggerRectify(job.Formation.key())
		}
		return true
	}
	return false
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

func (s *Scheduler) scheduleJobStart(job *Job) error {
	log := logger.New("fn", "scheduleJobStart")
	if !s.isLeader {
		return errors.New("this scheduler is not the leader")
	}
	if job.startedAt.Before(time.Now().Add(-s.backoffPeriod)) {
		log.Info("resetting job restarts", "backoffPeriod", s.backoffPeriod, "job.startedAt", job.startedAt)
		job.restarts = 0
	}
	backoff := s.getBackoffDuration(job.restarts)
	job.restarts += 1
	log.Info("scheduling job request", "attempts", job.restarts, "delay", backoff)
	time.AfterFunc(backoff, func() {
		s.jobRequests <- &JobRequest{Job: job}
	})
	return nil
}

func (s *Scheduler) getBackoffDuration(restarts uint) time.Duration {
	multiplier := 32 // max multiplier
	if restarts < 6 {
		// 2^(restarts - 1), or 0 if restarts == 0
		multiplier = (1 << restarts) >> 1
	}
	return s.backoffPeriod * time.Duration(multiplier)
}

func (s *Scheduler) handleJobEvent(job *Job, state JobState) *Job {
	log := logger.New("fn", "handleJobEvent", "job.id", job.JobID)
	if !s.jobs.IsJobInState(job.JobID, state) && job.IsSchedulable() {
		log.Info("marking job state", "state", state)
		s.jobs.AddJob(job)
		s.jobs.SetState(job.JobID, state)
		return s.jobs[job.JobID]
	}
	return nil
}

func (s *Scheduler) handleJobCrash(job *Job) {
	log := logger.New("fn", "handleJobCrash", "job.id", job.JobID, "job.restarts", job.restarts)
	j := s.handleJobEvent(job, JobStateCrashed)
	if j != nil {
		log.Info("attempting to restart crashed job")
		err := s.scheduleJobStart(j)
		if err != nil {
			log.Warn("failed to schedule job request, marking job as stopped")
			s.jobs.SetState(job.JobID, JobStateStopped)
		}
	}
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
	EventTypeJobRequest      EventType = "request-job"
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

type JobRequestEvent struct {
	Event
	Request *JobRequest
}

func (jre *JobRequestEvent) Data() interface{} {
	return jre.Request
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
	case EventTypeJobRequest:
		req, _ := data.(*JobRequest)
		return &JobRequestEvent{Event: &DefaultEvent{err: err, typ: typ}, Request: req}
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
