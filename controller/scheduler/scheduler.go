package main

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"
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
	eventBufferSize    int           = 1000
	maxJobAttempts     uint          = 30
	jobAttemptInterval time.Duration = 500 * time.Millisecond
)

var logger = log15.New("component", "scheduler")

func fnLogger(ctx ...interface{}) log15.Logger {
	pc := make([]uintptr, 10) // at least 1 entry needed
	runtime.Callers(2, pc)
	f := runtime.FuncForPC(pc[0])
	name := f.Name()
	name = name[strings.LastIndex(name, ".")+1:]
	params := []interface{}{"fn", name}
	params = append(params, ctx...)
	return logger.New(params...)
}

type Scheduler struct {
	utils.ControllerClient
	utils.ClusterClient

	discoverd Discoverd
	isLeader  bool

	backoffPeriod time.Duration

	formations  Formations
	hostStreams map[string]stream.Stream
	jobs        Jobs

	jobEvents chan *host.Event

	listeners map[chan Event]struct{}
	listenMtx sync.RWMutex

	stop     chan struct{}
	stopOnce sync.Once

	syncJobs        chan struct{}
	syncFormations  chan struct{}
	rectify         chan utils.FormationKey
	hostEvents      chan *discoverd.Event
	formationEvents chan *ct.ExpandedFormation
	jobRequests     chan *JobRequest
	putJobs         chan *ct.Job

	caseHandlers CaseHandlers
}

func NewScheduler(cluster utils.ClusterClient, cc utils.ControllerClient, disc Discoverd) *Scheduler {
	return &Scheduler{
		ControllerClient: cc,
		ClusterClient:    cluster,
		discoverd:        disc,
		backoffPeriod:    getBackoffPeriod(),
		hostStreams:      make(map[string]stream.Stream),
		jobs:             make(map[string]*Job),
		formations:       make(Formations),
		listeners:        make(map[chan Event]struct{}),
		jobEvents:        make(chan *host.Event, eventBufferSize),
		stop:             make(chan struct{}),
		syncJobs:         make(chan struct{}, 1),
		syncFormations:   make(chan struct{}, 1),
		rectify:          make(chan utils.FormationKey, eventBufferSize),
		formationEvents:  make(chan *ct.ExpandedFormation, eventBufferSize),
		hostEvents:       make(chan *discoverd.Event, eventBufferSize),
		jobRequests:      make(chan *JobRequest, eventBufferSize),
		putJobs:          make(chan *ct.Job, eventBufferSize),
	}
}

func main() {
	log := fnLogger()

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
	log := fnLogger()

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
	if err := connect(); err != nil {
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
	log := fnLogger()

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
	log := fnLogger()
	log.Info("starting scheduler loop")
	defer log.Info("scheduler loop exited")

	// stream host events (which will start watching job events on
	// all current hosts before returning) *before* registering in
	// service discovery so that there is always at least one scheduler
	// watching all job events, even during a deployment.
	if err := s.streamHostEvents(); err != nil {
		return err
	}

	var err error
	s.isLeader, err = s.discoverd.Register()
	if err != nil {
		return err
	}
	leaderCh := s.discoverd.LeaderCh()

	channels := map[reflect.Value]func(interface{}) error{
		reflect.ValueOf(s.stop): func(interface{}) error {
			return errors.New("stopped")
		},
		reflect.ValueOf(leaderCh): func(i interface{}) error {
			isLeader := i.(bool)
			s.HandleLeaderChange(isLeader)
			return nil
		},
		reflect.ValueOf(s.jobRequests): func(i interface{}) error {
			req := i.(*JobRequest)
			s.HandleJobRequest(req)
			return nil
		},
		reflect.ValueOf(s.hostEvents): func(i interface{}) error {
			e := i.(*discoverd.Event)
			s.HandleHostEvent(e)
			return nil
		},
		reflect.ValueOf(s.jobEvents): func(i interface{}) error {
			e := i.(*host.Event)
			s.HandleJobEvent(e)
			return nil
		},
		reflect.ValueOf(s.formationEvents): func(i interface{}) error {
			e := i.(*ct.ExpandedFormation)
			s.HandleFormationChange(e)
			return nil
		},
		reflect.ValueOf(s.syncFormations): func(interface{}) error {
			s.SyncFormations()
			return nil
		},
		reflect.ValueOf(s.syncJobs): func(interface{}) error {
			s.SyncJobs()
			return nil
		},
	}
	s.caseHandlers = make(CaseHandlers, 0, len(channels))
	for c, h := range channels {
		s.caseHandlers = append(s.caseHandlers, CaseHandler{
			sc: reflect.SelectCase{
				Dir:  reflect.SelectRecv,
				Chan: c,
			},
			handler: h,
		})
	}

	if err := s.streamFormationEvents(); err != nil {
		return err
	}

	s.tickSyncJobs(30 * time.Second)
	s.tickSyncFormations(time.Minute)

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

		// Finally, handle triggering cluster changes.
		// Re-select on all the channels so we don't have to sleep nor spin
		ch := s.rectifyCaseHandlers()
		err := ch.SelectAndHandle()
		if err != nil {
			log.Info("stopping scheduler loop")
			close(s.putJobs)
			return nil
		}
	}
	return nil
}

func (s *Scheduler) SyncJobs() {
	defer s.sendEvent(EventTypeClusterSync, nil, nil)

	log := fnLogger()
	log.Info("syncing jobs")

	log.Info("getting host list")
	hosts, err := s.getHosts()
	if err != nil {
		log.Error("error getting host list", "err", err)
		return
	}

	knownJobs := make(Jobs)
	for _, h := range hosts {
		hostLog := log.New("host.id", h.ID())

		hostLog.Info(fmt.Sprintf("getting jobs for host %s", h.ID()))
		activeJobs, err := h.ListJobs()
		if err != nil {
			hostLog.Error("error getting jobs list", "err", err)
			continue
		}
		hostLog.Info(fmt.Sprintf("got %d active job(s) for host %s", len(activeJobs), h.ID()))

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
		}
	}
}

func (s *Scheduler) SyncFormations() {
	defer s.sendEvent(EventTypeFormationSync, nil, nil)

	log := fnLogger()
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
		appLog.Info(fmt.Sprintf("got %d formation(s) for %s app", len(fs), app.Name))

		for _, f := range fs {
			_, err := s.updateFormation(f)
			if err != nil {
				appLog.Error("error updating formation", "release.id", f.ReleaseID, "err", err)
			}
		}
	}
}

func (s *Scheduler) HandleRectify(i interface{}) error {
	key := i.(utils.FormationKey)
	s.RectifyFormation(key)
	return nil
}

func (s *Scheduler) RectifyFormation(key utils.FormationKey) {
	log := fnLogger("app.id", key.AppID, "release.id", key.ReleaseID)
	formation := s.formations[key]
	if formation.IsEmpty() {
		log.Info("removing empty formation from memory")
		s.formations.Remove(key)
	}
	if !s.isLeader {
		return
	}

	defer s.sendEvent(EventTypeRectify, nil, key)

	expected := formation.GetProcesses()
	actual := s.jobs.GetProcesses(key)
	if expected.Equals(actual) {
		return
	}
	log.Info("rectifying formation")

	formation.Processes = actual
	diff := formation.Update(expected)
	log.Info("existing formation in incorrect state", "expected", expected, "actual", actual, "diff", diff)
	s.handleFormationDiff(formation, diff)
}

func (s *Scheduler) HandleFormationChange(ef *ct.ExpandedFormation) {
	var err error
	defer func() {
		s.sendEvent(EventTypeFormationChange, err, nil)
	}()

	log := fnLogger("app.id", ef.App.ID, "release.id", ef.Release.ID)
	log.Info("handling formation change")
	_, err = s.changeFormation(ef)
	if err != nil {
		log.Error("error handling formation change", "err", err)
		return
	}
}

func (s *Scheduler) HandleJobRequest(req *JobRequest) {
	log := fnLogger("req.id", req.JobID, "req.type", req.RequestType)

	if !s.isLeader {
		log.Warn("ignoring job request as not service leader")
		return
	}

	var err error
	defer func() {
		if err != nil {
			log.Error("error handling job request", "err", err)
		}
		s.sendEvent(EventTypeJobRequest, err, req)
	}()

	log.Info("handling job request")
	switch req.RequestType {
	case JobRequestTypeUp:
		err = s.startJob(req)
	case JobRequestTypeDown:
		err = s.stopJob(req)
	default:
		err = fmt.Errorf("unknown job request type: %s", req.RequestType)
	}
}

func (s *Scheduler) RunPutJobs() {
	log := fnLogger()
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
	log := fnLogger()
	s.isLeader = isLeader
	if isLeader {
		log.Info("handling leader promotion")
		s.rectifyAll()
	} else {
		log.Info("handling leader demotion")
		// TODO: stop job restart timers
	}
	s.sendEvent(EventTypeLeaderChange, nil, isLeader)
}

func (s *Scheduler) handleFormationDiff(f *Formation, diff Processes) {
	log := fnLogger("app.id", f.App.ID, "release.id", f.Release.ID)
	for typ, n := range diff {
		if n > 0 {
			log.Info(fmt.Sprintf("requesting %d new job(s) of type %s", n, typ))
			for i := 0; i < n; i++ {
				req := NewJobRequest(f, JobRequestTypeUp, typ, "", random.UUID())
				req.state = JobStateRequesting
				s.jobs.AddJob(req.Job)
				s.HandleJobRequest(req)
			}
		} else if n < 0 {
			log.Info(fmt.Sprintf("requesting removal of %d job(s) of type %s", -n, typ))
			for i := 0; i < -n; i++ {
				req := NewJobRequest(f, JobRequestTypeDown, typ, "", "")
				s.HandleJobRequest(req)
			}
		}
	}
}

func (s *Scheduler) followHost(h utils.HostClient) {
	if _, ok := s.hostStreams[h.ID()]; ok {
		return
	}

	log := fnLogger("host.id", h.ID())
	log.Info("streaming job events")
	events := make(chan *host.Event)
	stream, err := h.StreamEvents("all", events)
	if err != nil {
		log.Error("error streaming job events", "err", err)
		return
	}

	log.Info("getting active jobs")
	jobs, err := h.ListJobs()
	if err != nil {
		log.Error("error getting active jobs", "err", err)
		return
	}
	log.Info(fmt.Sprintf("got %d active job(s) for host %s", len(jobs), h.ID()))

	for _, job := range jobs {
		s.handleActiveJob(&job)
	}

	s.hostStreams[h.ID()] = stream

	s.triggerSyncFormations()

	go func() {
		for e := range events {
			s.jobEvents <- e
		}
		// TODO: reconnect this stream unless unfollowHost was called
		log.Error("job event stream closed unexpectedly")
	}()
}

func (s *Scheduler) unfollowHost(id string) {
	log := fnLogger("host.id", id)
	stream, ok := s.hostStreams[id]
	if !ok {
		log.Warn("ignoring host unfollow due to lack of existing stream")
		return
	}

	log.Info("unfollowing host")
	for jobID, job := range s.jobs {
		if job.HostID == id {
			log.Info("removing job", "job.id", jobID)
			s.jobs.SetState(job.JobID, JobStateStopped)
			s.triggerRectify(job.Formation.key())
		}
	}

	log.Info("closing job event stream")
	stream.Close()
	delete(s.hostStreams, id)

	s.triggerSyncFormations()
}

func (s *Scheduler) HandleHostEvent(e *discoverd.Event) {
	log := fnLogger("event.type", e.Kind)
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
		log = log.New("host.id", e.Instance.Meta["id"])
		log.Info("host is down, stopping job event stream")
		s.unfollowHost(e.Instance.Meta["id"])
	}
}

func (s *Scheduler) HandleJobEvent(e *host.Event) {
	log := fnLogger("job.id", e.JobID, "event.type", e.Event)

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
	log := fnLogger("job.id", job.ID, "app.id", appID, "release.id", releaseID, "job.type", jobType)

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
				f, err = s.updateFormation(cf)
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

func (s *Scheduler) changeFormation(ef *ct.ExpandedFormation) (f *Formation, err error) {
	if ef.App == nil {
		return nil, errors.New("formation has no app")
	} else if ef.Release == nil {
		return nil, errors.New("formation has no release")
	}

	log := fnLogger("app.id", ef.App.ID, "release.id", ef.Release.ID)

	for typ, proc := range ef.Release.Processes {
		if proc.Omni && ef.Processes != nil && ef.Processes[typ] > 0 {
			ef.Processes[typ] *= len(s.hostStreams)
		}
	}

	f = s.formations.Get(ef.App.ID, ef.Release.ID)
	if f == nil {
		log.Info("adding new formation", "processes", ef.Processes)
		f = s.formations.Add(NewFormation(ef, s.HandleRectify))
	} else {
		if f.GetProcesses().Equals(ef.Processes) {
			return f, nil
		} else {
			log.Info("updating processes of existing formation", "processes", ef.Processes)
			f.Processes = ef.Processes
		}
	}
	s.triggerRectify(f.key())
	return f, nil
}

func (s *Scheduler) triggerRectify(key utils.FormationKey) {
	logger.Info("triggering rectify", "key", key)
	s.formations.TriggerRectify(key)
}

func (s *Scheduler) updateFormation(f *ct.Formation) (*Formation, error) {
	ef, err := utils.ExpandFormation(s, f)
	if err != nil {
		return nil, err
	}
	return s.changeFormation(ef)
}

func (s *Scheduler) startJob(req *JobRequest) (err error) {
	log := fnLogger("job.type", req.Type)
	log.Info("starting job", "job.restarts", req.restarts, "request.attempts", req.attempts)
	s.jobs.SetState(req.JobID, JobStateStopped)
	// Copy on Write
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
	hostID := host.ID()
	newReq.HostID = hostID

	config := jobConfig(newReq, hostID)
	newReq.JobID = config.ID

	// Provision a data volume on the host if needed.
	if newReq.needsVolume() {
		log.Info("provisioning volume")
		if err := utils.ProvisionVolume(host, config); err != nil {
			log.Error("error provisioning volume", "err", err)
			return err
		}
	}

	log.Info("requesting host to add job", "host.id", hostID, "job.id", config.ID)
	if err := host.AddJob(config); err != nil {
		log.Error("error requesting host to add job", "err", err)
		return err
	}
	return nil
}

func (s *Scheduler) stopJob(req *JobRequest) (err error) {
	log := fnLogger("req.host.id", req.HostID, "req.job.id", req.JobID, "job.type", req.Type)
	log.Info("stopping job")
	defer func() {
		if err != nil {
			log.Error("error stopping job", "err", err)
		}
	}()

	var job *Job
	if req.JobID == "" {
		formationKey := utils.FormationKey{AppID: req.AppID, ReleaseID: req.ReleaseID}
		typJobs := s.jobs.GetStoppableJobs(formationKey, req.Type)
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
		log.Info("selected job for termination", "job.id", job.JobID, "job.host.id", job.HostID)
	} else {
		var ok bool
		job, ok = s.jobs[req.JobID]
		if !ok {
			e := "unknown job"
			log.Error(e)
			return errors.New(e)
		}
	}
	s.jobs.SetState(job.JobID, JobStateStopped)
	if job.HostID != "" {
		log.Info("getting host client", "host.id", job.HostID)
		host, err := s.Host(job.HostID)
		if err != nil {
			log.Error("error getting host client", "err", err)
			// TODO stop unfollowing hosts here once host syncing is built
			s.unfollowHost(job.HostID)
			return err
		}

		log.Info("requesting host to stop job")
		if err := host.StopJob(job.JobID); err != nil {
			log.Error("error requesting host to stop job", "err", err)
			return err
		}
	}
	return nil
}

func jobConfig(req *JobRequest, hostID string) *host.Job {
	return utils.JobConfig(req.Job.Formation.ExpandedFormation, req.Type, hostID)
}

func (s *Scheduler) findBestHost(formation *Formation, typ string) (utils.HostClient, error) {
	log := fnLogger("app.id", formation.App.ID, "release.id", formation.Release.ID, "job.type", typ)
	log.Info("getting host list")
	hosts, err := s.getHosts()
	if err != nil {
		log.Error("error getting host list", "err", err)
		return nil, err
	}

	counts := s.jobs.GetHostJobCounts(formation.key(), typ)
	var minCount int = math.MaxInt32
	var hostID string
	for _, host := range hosts {
		count, ok := counts[host.ID()]
		if !ok || count < minCount {
			minCount = count
			hostID = host.ID()
		}
	}
	if hostID == "" {
		return nil, fmt.Errorf("Unable to find a host out of %d host(s)", len(hosts))
	}
	log.Info(fmt.Sprintf("using host with least %s jobs", typ), "host.id", hostID)
	return s.Host(hostID)
}

func (s *Scheduler) getHosts() ([]utils.HostClient, error) {
	hosts, err := s.Hosts()
	if err != nil {
		return nil, err
	}

	// Ensure that we're only following hosts that we can discover
	knownHosts := make(map[string]struct{})
	for id, hostStream := range s.hostStreams {
		if hostStream.Err() == nil {
			knownHosts[id] = struct{}{}
		} else {
			// TODO stop unfollowing hosts here once host syncing is built
			s.unfollowHost(id)
		}
	}
	for _, h := range hosts {
		hostID := h.ID()
		delete(knownHosts, hostID)
		if _, ok := s.hostStreams[hostID]; !ok {
			s.followHost(h)
		}
	}
	for id := range knownHosts {
		// TODO stop unfollowing hosts here once host syncing is built
		s.unfollowHost(id)
	}
	if len(hosts) == 0 {
		log := fnLogger()
		e := "no hosts found"
		log.Error(e)
		return nil, errors.New(e)
	}

	return hosts, nil
}

func (s *Scheduler) Stop() error {
	log := fnLogger()
	log.Info("stopping scheduler loop")
	s.stopOnce.Do(func() {
		close(s.stop)
	})
	return nil
}

func (s *Scheduler) Subscribe(events chan Event) stream.Stream {
	log := fnLogger()
	log.Info("adding event subscriber")
	s.listenMtx.Lock()
	defer s.listenMtx.Unlock()
	s.listeners[events] = struct{}{}
	return &Stream{s, events}
}

func (s *Scheduler) Unsubscribe(events chan Event) {
	log := fnLogger()
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
		if job.IsStopped() {
			s.handleJobEvent(job, JobStateStopped)
		} else {
			s.handleJobCrash(job)
		}
	}
	if !s.jobs.IsJobInState(job.JobID, job.state) {
		log := fnLogger("job.id", job.JobID, "app.id", job.AppID, "app.name", appName, "release.id", job.ReleaseID, "job.type", job.Type, "job.status", status)
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
	jobs := make(map[string]*Job)
	for id, j := range s.jobs {
		if j.IsRunning() {
			jobs[id] = j
		}
	}
	return jobs
}

func (s *Scheduler) scheduleJobStart(job *Job) error {
	log := fnLogger()
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
		s.jobRequests <- &JobRequest{Job: job, RequestType: JobRequestTypeUp}
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
	log := fnLogger("job.id", job.JobID)
	if !s.jobs.IsJobInState(job.JobID, state) && job.IsSchedulable() {
		log.Info("marking job state", "state", state)
		s.jobs.AddJob(job)
		s.jobs.SetState(job.JobID, state)
		return s.jobs[job.JobID]
	}
	return nil
}

func (s *Scheduler) handleJobCrash(job *Job) {
	log := fnLogger("job.id", job.JobID, "job.restarts", job.restarts)
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
	log := fnLogger()
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

func (s *Scheduler) rectifyCaseHandlers() CaseHandlers {
	cases := make(CaseHandlers, 0, len(s.caseHandlers)+len(s.formations))
	cases = append(cases, s.caseHandlers...)
	cases = append(cases, s.formations.CaseHandlers()...)
	return cases
}

type CaseHandler struct {
	sc      reflect.SelectCase
	handler func(interface{}) error
}

type CaseHandlers []CaseHandler

func (cs CaseHandlers) SelectAndHandle() error {
	cases := make([]reflect.SelectCase, 0, len(cs))
	for _, c := range cs {
		cases = append(cases, c.sc)
	}

	chosen, recv, _ := reflect.Select(cases)
	return cs.handle(chosen, recv.Interface())
}

func (cs CaseHandlers) handle(i int, data interface{}) error {
	if i >= len(cs) {
		return errors.New("Index out of bounds")
	}
	c := cs[i]
	if c.handler != nil {
		return cs[i].handler(data)
	}
	return nil
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
