package main

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"reflect"
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

var logger = log15.New("component", "scheduler")

type Scheduler struct {
	utils.ControllerClient
	utils.ClusterClient

	isLeader      bool
	changeLeader  chan bool
	backoffPeriod time.Duration

	formations    Formations
	hostStreams   map[string]stream.Stream
	jobs          map[string]*Job
	stoppedJobs   map[string]*Job
	pendingStarts pendingJobs

	jobEvents chan *host.Event

	listeners map[chan Event]struct{}
	listenMtx sync.RWMutex

	stop     chan struct{}
	stopOnce sync.Once

	rectify         chan struct{}
	syncJobs        chan struct{}
	syncFormations  chan struct{}
	hostEvents      chan *discoverd.Event
	formationEvents chan *ct.ExpandedFormation
	jobRequests     chan *JobRequest
	putJobs         chan *ct.Job
}

func NewScheduler(cluster utils.ClusterClient, cc utils.ControllerClient) *Scheduler {
	return &Scheduler{
		ControllerClient: cc,
		ClusterClient:    cluster,
		changeLeader:     make(chan bool),
		backoffPeriod:    getBackoffPeriod(),
		hostStreams:      make(map[string]stream.Stream),
		jobs:             make(map[string]*Job),
		stoppedJobs:      make(map[string]*Job),
		pendingStarts:    make(pendingJobs),
		formations:       make(Formations),
		jobEvents:        make(chan *host.Event, eventBufferSize),
		listeners:        make(map[chan Event]struct{}),
		stop:             make(chan struct{}),
		rectify:          make(chan struct{}, 1),
		syncJobs:         make(chan struct{}, 1),
		syncFormations:   make(chan struct{}, 1),
		formationEvents:  make(chan *ct.ExpandedFormation, eventBufferSize),
		hostEvents:       make(chan *discoverd.Event, eventBufferSize),
		jobRequests:      make(chan *JobRequest, eventBufferSize),
		putJobs:          make(chan *ct.Job, eventBufferSize),
	}
}

func main() {
	log := logger.New("fn", "main")

	log.Info("creating cluster and controller clients")
	clusterClient := utils.ClusterClientWrapper(cluster.NewClient())
	controllerClient, err := controller.NewClient("", os.Getenv("AUTH_KEY"))
	if err != nil {
		log.Error("error creating controller client", "err", err)
		shutdown.Fatal(err)
	}
	s := NewScheduler(clusterClient, controllerClient)

	log.Info("registering with service discovery")
	hb, err := discoverd.AddServiceAndRegister("controller-scheduler", ":"+os.Getenv("PORT"))
	if err != nil {
		log.Error("error registering with service discovery", "err", err)
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { hb.Close() })

	log.Info("watching service discovery leaders")
	leaders := make(chan *discoverd.Instance)
	// TODO: reconect this stream on error
	stream, err := discoverd.NewService("controller-scheduler").Leaders(leaders)
	if err != nil {
		log.Error("error watching service discovery leaders", "err", err)
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { stream.Close() })
	go s.handleLeaderStream(leaders, hb.Addr())

	go s.startHTTPServer(os.Getenv("PORT"))

	if err := s.Run(); err != nil {
		shutdown.Fatal(err)
	}
	shutdown.Exit()
}

func (s *Scheduler) Run() error {
	log := logger.New("fn", "Run")
	log.Info("starting scheduler loop")
	defer log.Info("scheduler loop exited")

	s.tickSyncJobs(30 * time.Second)
	s.tickSyncFormations(time.Minute)

	log.Info("creating host event stream")
	// TODO: reconnect this stream
	stream, err := s.StreamHostEvents(s.hostEvents)
	if err != nil {
		log.Error("error creating host event stream", "err", err)
		return err
	}
	defer stream.Close()

	log.Info("creating formation event stream")
	// TODO: reconnect this stream
	stream, err = s.StreamFormations(nil, s.formationEvents)
	if err != nil {
		log.Error("error creating formation event stream", "err", err)
		return err
	}
	defer stream.Close()

	go s.RunPutJobs()

	for {
		select {
		case <-s.stop:
			log.Info("stopping scheduler loop")
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
			s.HandleJobRequest(req)
			continue
		case e := <-s.hostEvents:
			s.HandleHostEvent(e)
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
		default:
		}

		// Finally, handle triggering cluster changes
		select {
		case <-s.rectify:
			s.Rectify()
		case <-time.After(10 * time.Millisecond):
			// block so that
			//	1) mutate events are given a chance to happen
			//  2) we don't spin hot
		}
	}
	return nil
}

func (s *Scheduler) SyncJobs() {
	defer s.sendEvent(NewEvent(EventTypeClusterSync, nil, nil))

	log := logger.New("fn", "SyncJobs")
	log.Info("syncing jobs")

	log.Info("getting host list")
	hosts, err := s.getHosts()
	if err != nil {
		log.Error("error getting host list", "err", err)
		return
	}

	newJobs := make(map[string]*Job)
	for _, h := range hosts {
		log = log.New("host.id", h.ID())

		log.Info(fmt.Sprintf("getting jobs for host %s", h.ID()))
		activeJobs, err := h.ListJobs()
		if err != nil {
			log.Error("error getting jobs list", "err", err)
			continue
		}
		log.Info(fmt.Sprintf("got %d active job(s) for host %s", len(activeJobs), h.ID()))

		for _, job := range activeJobs {
			s.handleActiveJob(&job)
			if j, ok := s.jobs[job.Job.ID]; ok {
				newJobs[j.JobID] = s.jobs[j.JobID]
			}
		}
	}

	s.jobs = newJobs

	s.triggerRectify()
}

func (s *Scheduler) SyncFormations() {
	defer s.sendEvent(NewEvent(EventTypeFormationSync, nil, nil))

	log := logger.New("fn", "SyncFormations")
	log.Info("syncing formations")

	log.Info("getting app list")
	apps, err := s.AppList()
	if err != nil {
		log.Error("error getting apps", "err", err)
		return
	}

	for _, app := range apps {
		log = log.New("app.id", app.ID)

		log.Info(fmt.Sprintf("getting formations for %s app", app.Name))
		fs, err := s.FormationList(app.ID)
		if err != nil {
			log.Error("error getting formations", "err", err)
			continue
		}
		log.Info(fmt.Sprintf("got %d formation(s) for %s app", len(fs), app.Name))

		for _, f := range fs {
			log.Info("updating formation", "release.id", f.ReleaseID)
			if _, err := s.updateFormation(f); err != nil {
				log.Error("error updating formation", "release.id", f.ReleaseID, "err", err)
			}
		}
	}

	s.triggerRectify()
}

func (s *Scheduler) Rectify() {
	log := logger.New("fn", "Rectify")

	if !s.isLeader {
		log.Warn("ignoring rectify as not service leader")
		return
	}

	defer s.sendEvent(NewEvent(EventTypeRectify, nil, nil))

	log.Info("starting rectify")

	pending := NewPendingJobs(s.jobs)
	pending.Update(s.pendingStarts)

	for key := range pending {
		formationLog := log.New("app.id", key.AppID, "release.id", key.ReleaseID)
		formationLog.Info("rectifying formation")
		formation, ok := s.formations[key]
		if !ok {
			formationLog.Info("unknown formation, getting from the controller")
			cf, err := s.GetFormation(key.AppID, key.ReleaseID)
			if err != nil {
				formationLog.Error("error getting formation", "err", err)
				continue
			}
			formation, err = s.updateFormation(cf)
			if err != nil {
				formationLog.Error("error updating formation", "err", err)
				continue
			}
		}

		expected := formation.Processes
		actual := pending.GetProcesses(key)

		if len(actual) == 0 && len(expected) == 0 || reflect.DeepEqual(actual, expected) {
			formationLog.Info("formation in correct state", "expected", expected, "actual", actual)
			continue
		}
		// TODO: make this formation.Diff(actual)?
		formation.Processes = actual
		diff := formation.Update(expected)
		formationLog.Info("formation in incorrect state", "expected", expected, "actual", actual, "diff", diff)
		s.sendDiffRequests(formation, diff)
	}

	for key, formation := range s.formations {
		if _, ok := pending[key]; !ok && len(formation.Processes) > 0 {
			log = log.New("app.id", key.AppID, "release.id", key.ReleaseID)
			log.Info("formation in incorrect state", "expected", formation.Processes, "actual", nil, "diff", formation.Processes)
			s.sendDiffRequests(formation, formation.Processes)
		}
	}
}

func (s *Scheduler) HandleFormationChange(ef *ct.ExpandedFormation) {
	var err error
	defer func() {
		s.sendEvent(NewEvent(EventTypeFormationChange, err, nil))
	}()

	log := logger.New("fn", "HandleFormationChange")
	if ef.App != nil {
		log = log.New("app.id", ef.App.ID)
	}
	if ef.Release != nil {
		log = log.New("release.id", ef.Release.ID)
	}
	log.Info("handling formation change")
	_, err = s.changeFormation(ef)
	if err != nil {
		log.Error("error handling formation change", "err", err)
		return
	}

	// Trigger sync jobs in case we've ignored an existing job because we
	// didn't know about the formation.
	s.triggerSyncJobs()
}

func (s *Scheduler) HandleJobRequest(req *JobRequest) {
	log := logger.New("fn", "HandleJobRequest", "req.id", req.JobID, "req.type", req.RequestType)

	if !s.isLeader {
		log.Warn("ignoring job request as not service leader")
		return
	}

	var err error
	defer func() {
		if err != nil {
			log.Error("error handling job request", "err", err)
		}
		s.sendEvent(NewEvent(EventTypeJobRequest, err, req))
	}()

	log.Info("handling job request")
	switch req.RequestType {
	case JobRequestTypeUp:
		// startJob sets the HostID on the request if successful
		s.pendingStarts.RemoveJob(req.Job)
		err = s.startJob(req)
		s.pendingStarts.AddJob(req.Job)
	default:
		err = fmt.Errorf("unknown job request type: %s", req.RequestType)
	}
}

func (s *Scheduler) RunPutJobs() {
	log := logger.New("fn", "RunPutJobs")
	log.Info("starting job persistence loop")
	strategy := attempt.Strategy{Delay: 100 * time.Millisecond, Total: time.Minute}
	for {
		job, ok := <-s.putJobs
		if !ok {
			log.Info("stopping job persistence loop")
			return
		}
		jobLog := log.New("job.id", job.ID, "job.state", job.State)
		jobLog.Info("persisting job")
		err := strategy.Run(func() error {
			return s.PutJob(job)
		})
		if err != nil {
			jobLog.Error("error persisting job", "err", err)
		}
	}
}

func (s *Scheduler) ChangeLeader(isLeader bool) {
	s.changeLeader <- isLeader
}

func (s *Scheduler) HandleLeaderChange(isLeader bool) {
	log := logger.New("fn", "HandleLeaderChange")
	s.isLeader = isLeader
	if isLeader {
		log.Info("handling leader promotion")
		s.triggerRectify()
	} else {
		log.Info("handling leader demotion")
		// TODO: stop job restart timers
	}
	s.sendEvent(NewEvent(EventTypeLeaderChange, nil, isLeader))
}

func (s *Scheduler) sendDiffRequests(f *Formation, diff map[string]int) {
	log := logger.New("fn", "sendDiffRequests", "app.id", f.App.ID, "release.id", f.Release.ID)
	for typ, n := range diff {
		if n > 0 {
			log.Info(fmt.Sprintf("requesting %d new job(s) of type %s", n, typ))
			for i := 0; i < n; i++ {
				req := NewJobRequest(f, JobRequestTypeUp, typ, "", "")
				s.startJob(req)
				// startJob sets the HostID on the request if successful,
				// otherwise the HostID will be blank
				s.pendingStarts.AddJob(req.Job)
			}
		} else if n < 0 {
			log.Info(fmt.Sprintf("requesting removal of %d job(s) of type %s", -n, typ))
			for i := 0; i < -n; i++ {
				req := NewJobRequest(f, JobRequestTypeDown, typ, "", "")
				s.stopJob(req)
			}
		}
	}
}

func (s *Scheduler) followHost(h utils.HostClient) {
	_, ok := s.hostStreams[h.ID()]
	if ok {
		return
	}

	log := logger.New("fn", "followHost", "host.id", h.ID())
	log.Info("streaming job events")
	// TODO: reconnect this stream, stopping only if unfollowHost is called
	events := make(chan *host.Event)
	stream, err := h.StreamEvents("all", events)
	if err != nil {
		log.Error("error streaming job events", "err", err)
		return
	}
	s.hostStreams[h.ID()] = stream

	s.triggerSyncFormations()

	go func() {
		for {
			e, ok := <-events
			if !ok {
				log.Error("job event stream closed unexpectedly")
				return
			}
			s.jobEvents <- e
		}
	}()
}

func (s *Scheduler) unfollowHost(id string) {
	log := logger.New("fn", "unfollowHost", "host.id", id)
	stream, ok := s.hostStreams[id]
	if !ok {
		log.Warn("ignoring host unfollow due to lack of existing stream")
		return
	}

	log.Info("unfollowing host")
	for jobID, job := range s.jobs {
		if job.HostID == id {
			log.Info("removing job", "job.id", jobID)
			delete(s.jobs, jobID)
		}
	}

	log.Info("closing job event stream")
	stream.Close()
	delete(s.hostStreams, id)

	s.triggerSyncFormations()
}

func (s *Scheduler) HandleHostEvent(e *discoverd.Event) {
	log := logger.New("fn", "HandleHostEvent")

	// Sometimes events are missing e.Instance or e.Instance.Meta,
	// in which case we can get a panic by not checking it first
	if e == nil || e.Instance == nil || e.Instance.Meta == nil {
		log.Error(fmt.Sprintf("ignoring invalid host event: %+v", e))
		return
	}
	hostID, ok := e.Instance.Meta["id"]
	if !ok {
		log.Warn("ignoring host event due to missing ID in service metadata")
		return
	}

	log = log.New("host.id", hostID, "event.type", e.Kind)
	log.Info("handling host event")

	var err error
	defer func() {
		s.sendEvent(NewEvent(EventTypeHostEvent, err, nil))
	}()

	switch e.Kind {
	case discoverd.EventKindUp:
		log.Info("host is up, starting job event stream")
		var h utils.HostClient
		h, err = s.Host(hostID)
		if err != nil {
			log.Error("error creating host client", "err", err)
			return
		}
		s.followHost(h)
	case discoverd.EventKindDown:
		log.Info("host is down, stopping job event stream")
		s.unfollowHost(hostID)
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
		s.sendEvent(NewEvent(EventTypeJobStart, err, job))
	case host.JobEventStop:
		s.sendEvent(NewEvent(EventTypeJobStop, err, job))
	}

	if err == nil {
		s.triggerRectify()
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
	log.Info("handling active job")

	j, ok := s.jobs[job.ID]
	if !ok {
		if j, ok = s.stoppedJobs[job.ID]; !ok {
			log.Info("job is new, looking up formation")
			f := s.formations.Get(appID, releaseID)
			if f == nil {
				log.Info("job is from new formation, getting formation from controller")
				cf, err := s.GetFormation(appID, releaseID)
				if err != nil {
					log.Error("error getting formation", "err", err)
					return nil, err
				}
				f, err = s.updateFormation(cf)
				if err != nil {
					log.Error("error updating formation", "err", err)
					return nil, err
				}
			}
			j = NewJob(f, jobType, activeJob.HostID, job.ID, activeJob.StartedAt)
		}
	}
	s.SaveJob(j, appName, activeJob.Status, utils.JobMetaFromMetadata(job.Metadata))
	return j, nil
}

func (s *Scheduler) changeFormation(ef *ct.ExpandedFormation) (*Formation, error) {
	if ef.App == nil {
		return nil, errors.New("formation has no app")
	} else if ef.Release == nil {
		return nil, errors.New("formation has no release")
	}

	log := logger.New("fn", "changeFormation", "app.id", ef.App.ID, "release.id", ef.Release.ID)

	log.Info("expanding omni process types")
	for typ, proc := range ef.Release.Processes {
		if proc.Omni && ef.Processes != nil && ef.Processes[typ] > 0 {
			ef.Processes[typ] *= len(s.hostStreams)
		}
	}

	f := s.formations.Get(ef.App.ID, ef.Release.ID)
	if f == nil {
		log.Info("adding new formation", "processes", ef.Processes)
		f = s.formations.Add(NewFormation(ef))
	} else {
		log.Info("updating processes of existing formation", "processes", ef.Processes)
		f.Processes = ef.Processes
	}
	return f, nil
}

func (s *Scheduler) updateFormation(f *ct.Formation) (*Formation, error) {
	ef, err := utils.ExpandFormation(s, f)
	if err != nil {
		return nil, err
	}
	return s.changeFormation(ef)
}

func (s *Scheduler) startJob(req *JobRequest) (err error) {
	log := logger.New("fn", "startJob", "job.type", req.Type, "job.restarts", req.restarts)
	log.Info("starting job")
	defer func() {
		if err != nil {
			s.scheduleJobRequest(req)
			log.Error("error starting job", "err", err)
		}
	}()

	log.Info("determining best host for job")
	host, err := s.findBestHost(req.Formation, req.Type, req.HostID)
	if err != nil {
		log.Error("error determining best host for job", "err", err)
		return err
	}

	config := jobConfig(req, host.ID())

	// Provision a data volume on the host if needed.
	if req.needsVolume() {
		log.Info("provisioning volume")
		if err := utils.ProvisionVolume(host, config); err != nil {
			log.Error("error provisioning volume", "err", err)
			return err
		}
	}

	log.Info("requesting host to add job", "host.id", host.ID(), "job.id", config.ID)
	if err := host.AddJob(config); err != nil {
		log.Error("error requesting host to add job", "err", err)
		return err
	}
	req.HostID = host.ID()
	return nil
}

func (s *Scheduler) stopJob(req *JobRequest) (err error) {
	log := logger.New("fn", "stopJob", "host.id", req.HostID, "job.id", req.JobID, "job.type", req.Type)
	log.Info("stopping job")
	defer func() {
		if err != nil {
			log.Error("error stopping job", "err", err)
		}
	}()

	var job *Job
	if req.JobID == "" {
		formationKey := utils.FormationKey{AppID: req.AppID, ReleaseID: req.ReleaseID}
		formJobs := NewFormationJobs(s.jobs)
		typJobs := formJobs[formationKey][req.Type]
		if len(typJobs) == 0 {
			e := fmt.Sprintf("no %s jobs running", req.Type)
			log.Error(e)
			return errors.New(e)
		}
		job = typJobs[0]
		for _, j := range typJobs {
			if j.startedAt.After(job.startedAt) {
				job = j
			}
		}
	} else {
		var ok bool
		job, ok = s.jobs[req.JobID]
		if !ok {
			e := "unknown job"
			log.Error(e)
			return errors.New(e)
		}
	}

	log.Info("getting host client")
	host, err := s.Host(job.HostID)
	if err != nil {
		log.Error("error getting host client", "err", err)
		s.unfollowHost(job.HostID)
		return err
	}

	log.Info("requesting host to stop job")
	if err := host.StopJob(job.JobID); err != nil {
		log.Error("error requesting host to stop job", "err", err)
		return err
	}
	s.stoppedJobs[job.JobID] = job
	delete(s.jobs, job.JobID)
	return nil
}

func jobConfig(req *JobRequest, hostID string) *host.Job {
	return utils.JobConfig(req.Job.Formation.ExpandedFormation, req.Type, hostID)
}

func (s *Scheduler) findBestHost(formation *Formation, typ, hostID string) (utils.HostClient, error) {
	log := logger.New("fn", "findBestHost", "app.id", formation.App.ID, "release.id", formation.Release.ID, "job.type", typ)
	log.Info("getting host list")
	hosts, err := s.getHosts()
	if err != nil {
		log.Error("error getting host list", "err", err)
		return nil, err
	}
	if len(hosts) == 0 {
		e := "no hosts found"
		log.Error(e)
		return nil, errors.New(e)
	}

	if hostID != "" {
		log.Info("using explicit host", "host.id", hostID)
		return s.Host(hostID)
	}

	pending := NewPendingJobs(s.jobs)
	pending.Update(s.pendingStarts)
	counts := pending.GetHostJobCounts(formation.key(), typ)
	var minCount int = math.MaxInt32
	for _, host := range hosts {
		count, ok := counts[host.ID()]
		if !ok || count < minCount {
			minCount = count
			hostID = host.ID()
		}
	}
	logger.Info(fmt.Sprintf("using host with least %s jobs", typ), "host.id", hostID)
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
	logger.Info("stopping scheduler loop", "fn", "Stop")
	s.stopOnce.Do(func() {
		close(s.stop)
	})
	return nil
}

func (s *Scheduler) Subscribe(events chan Event) stream.Stream {
	logger.Info("adding event subscriber", "fn", "Subscribe")
	s.listenMtx.Lock()
	defer s.listenMtx.Unlock()
	s.listeners[events] = struct{}{}
	return &Stream{s, events}
}

func (s *Scheduler) Unsubscribe(events chan Event) {
	logger.Info("removing event subscriber", "fn", "Unsubscribe")
	s.listenMtx.Lock()
	defer s.listenMtx.Unlock()
	delete(s.listeners, events)
}

func (s *Scheduler) SaveJob(job *Job, appName string, status host.JobStatus, metadata map[string]string) (*Job, error) {
	controllerState := "down"
	switch status {
	case host.StatusStarting:
		fallthrough
	case host.StatusRunning:
		s.handleJobStart(job)
		controllerState = "up"
	default:
		delete(s.jobs, job.JobID)
		delete(s.stoppedJobs, job.JobID)
	}
	s.putJobs <- controllerJobFromSchedulerJob(job, controllerState, metadata)
	return job, nil
}

func (s *Scheduler) Jobs() map[string]*Job {
	jobs := make(map[string]*Job)
	for id, j := range s.jobs {
		jobs[id] = j
	}
	return jobs
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
	req.restarts += 1
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
	log := logger.New("fn", "handleJobStart", "job.id", job.JobID)
	log.Info("adding job to in-memory state")
	_, ok := s.jobs[job.JobID]
	if s.isLeader && !ok && s.pendingStarts.HasStarts(job) {
		s.pendingStarts.RemoveJob(job)
	}
	s.jobs[job.JobID] = job
}

func (s *Scheduler) handleLeaderStream(leaders chan *discoverd.Instance, selfAddr string) {
	log := logger.New("fn", "handleLeaderStream")
	for leader := range leaders {
		log.Info("received leader event", "leader.addr", leader.Addr, "self.addr", selfAddr)
		s.ChangeLeader(leader.Addr == selfAddr)
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
		ch := time.Tick(d)
		for range ch {
			s.triggerSyncJobs()
		}
	}()
}

func (s *Scheduler) tickSyncFormations(d time.Duration) {
	logger.Info("starting sync formations ticker", "duration", d)
	go func() {
		ch := time.Tick(d)
		for range ch {
			s.triggerSyncFormations()
		}
	}()
}

func (s *Scheduler) triggerRectify() {
	logger.Info("triggering rectify")
	select {
	case s.rectify <- struct{}{}:
	default:
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
	logger.Info(fmt.Sprintf("sending %s event to %d listener(s)", event.Type(), len(s.listeners)), "event", event.Type(), "err", event.Err())
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

type JobEvent struct {
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
