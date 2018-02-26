package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"sync"
	"time"

	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	discoverd "github.com/flynn/flynn/discoverd/client"
	host "github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/flynn/flynn/pkg/typeconv"
	"github.com/flynn/flynn/router/types"
	"github.com/inconshreveable/log15"
)

const (
	eventBufferSize        = 1000
	defaultMaxHostChecks   = 10
	routerDrainTimeout     = 10 * time.Second
	routerBackendUpTimeout = 10 * time.Second
)

var (
	ErrNotLeader        = errors.New("scheduler is not the leader")
	ErrNoHosts          = errors.New("no hosts found")
	ErrJobNotPending    = errors.New("job is no longer pending")
	ErrNoHostsMatchTags = errors.New("no hosts found matching job tags")
	ErrHostIsDown       = errors.New("host is down")
)

type Scheduler struct {
	utils.ControllerClient
	utils.ClusterClient

	discoverd Discoverd
	isLeader  *bool

	logger log15.Logger

	maxHostChecks int

	formations Formations
	sinks      map[string]*ct.Sink
	hosts      map[string]*Host
	routers    map[string]*Router
	jobs       Jobs
	volumes    map[string]*Volume
	routes     map[string]map[string]struct{}
	services   map[string]*Service

	jobEvents    chan *host.Event
	volumeEvents chan *VolumeEvent

	stop     chan struct{}
	stopOnce sync.Once

	syncJobs              chan struct{}
	syncFormations        chan struct{}
	syncSinks             chan struct{}
	syncVolumes           chan struct{}
	syncHosts             chan struct{}
	hostChecks            chan struct{}
	rectify               chan struct{}
	sendTelemetry         chan struct{}
	hostEvents            chan *discoverd.Event
	serviceEvents         chan *discoverd.Event
	routerServiceEvents   chan *discoverd.Event
	routerStreamEvents    chan *RouterEvent
	formationEvents       chan *ct.ExpandedFormation
	sinkEvents            chan *ct.Sink
	controllerPersist     chan interface{}
	placementRequests     chan *PlacementRequest
	internalStateRequests chan *InternalStateRequest

	rectifyBatch map[utils.FormationKey]struct{}

	// formationlessJobs is a map of formation keys to a list of jobs
	// which are in-memory but do not have a formation (because the
	// formation lookup failed when we got an event for the job), and is
	// used to update the jobs once we get the formation during a sync
	// so that we can determine if the job should actually be running
	formationlessJobs map[utils.FormationKey]map[string]*Job

	// pause and resume are used by tests to control the main loop
	pause  chan struct{}
	resume chan struct{}

	// generateJobUUID generates a UUID for new job IDs and is overridden in tests
	// to make them more predictable
	generateJobUUID func() string

	routerBackends map[string]*RouterBackend
}

func NewScheduler(cluster utils.ClusterClient, cc utils.ControllerClient, disc Discoverd, l log15.Logger) *Scheduler {
	return &Scheduler{
		ControllerClient:      cc,
		ClusterClient:         cluster,
		discoverd:             disc,
		logger:                l,
		maxHostChecks:         defaultMaxHostChecks,
		hosts:                 make(map[string]*Host),
		routers:               make(map[string]*Router),
		jobs:                  make(map[string]*Job),
		volumes:               make(map[string]*Volume),
		services:              make(map[string]*Service),
		routes:                make(map[string]map[string]struct{}),
		formations:            make(Formations),
		sinks:                 make(map[string]*ct.Sink),
		jobEvents:             make(chan *host.Event, eventBufferSize),
		volumeEvents:          make(chan *VolumeEvent, eventBufferSize),
		stop:                  make(chan struct{}),
		syncJobs:              make(chan struct{}, 1),
		syncFormations:        make(chan struct{}, 1),
		syncSinks:             make(chan struct{}, 1),
		syncVolumes:           make(chan struct{}, 1),
		syncHosts:             make(chan struct{}, 1),
		hostChecks:            make(chan struct{}, 1),
		rectifyBatch:          make(map[utils.FormationKey]struct{}),
		rectify:               make(chan struct{}, 1),
		sendTelemetry:         make(chan struct{}, 1),
		formationEvents:       make(chan *ct.ExpandedFormation, eventBufferSize),
		hostEvents:            make(chan *discoverd.Event, eventBufferSize),
		serviceEvents:         make(chan *discoverd.Event, eventBufferSize),
		routerServiceEvents:   make(chan *discoverd.Event, eventBufferSize),
		routerStreamEvents:    make(chan *RouterEvent, eventBufferSize),
		sinkEvents:            make(chan *ct.Sink, eventBufferSize),
		controllerPersist:     make(chan interface{}, eventBufferSize),
		placementRequests:     make(chan *PlacementRequest, eventBufferSize),
		internalStateRequests: make(chan *InternalStateRequest, eventBufferSize),
		formationlessJobs:     make(map[utils.FormationKey]map[string]*Job),
		pause:                 make(chan struct{}),
		resume:                make(chan struct{}),
		generateJobUUID:       random.UUID,
		routerBackends:        make(map[string]*RouterBackend),
	}
}

func main() {
	logger := log15.New("component", "scheduler")
	logger.SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StdoutHandler))
	log := logger.New("fn", "main")

	// Use a low timeout for HTTP requests to avoid blocking the main loop.
	//
	// TODO: make all HTTP calls asynchronous
	//       (see https://github.com/flynn/flynn/issues/1920)
	httpClient := &http.Client{Timeout: 10 * time.Second}

	log.Info("creating cluster and controller clients")
	clusterClient := utils.ClusterClientWrapper(cluster.NewClientWithHTTP(nil, httpClient))
	controllerClient, err := controller.NewClient("", os.Getenv("AUTH_KEY"))
	if err != nil {
		log.Error("error creating controller client", "err", err)
		shutdown.Fatal(err)
	}

	log.Info("waiting for controller API to come up")
	if _, err := discoverd.GetInstances("controller", 5*time.Minute); err != nil {
		log.Error("error waiting for controller API", "err", err)
		shutdown.Fatal(err)
	}

	s := NewScheduler(clusterClient, controllerClient, newDiscoverdWrapper(logger), logger)
	log.Info("started scheduler")

	go s.startHTTPServer(os.Getenv("PORT"))

	s.Run()
	shutdown.Exit()
}

func (s *Scheduler) streamFormationEvents() {
	log := s.logger.New("fn", "streamFormationEvents")

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

	current := make(chan struct{})
	go func() {
		var isCurrent bool
		for {
			for {
				if err := connect(); err == nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
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
		}
	}()

	// wait until we have the current list of formations before
	// starting the main scheduler loop
	start := time.Now()
	tick := time.Tick(30 * time.Second)
	for {
		select {
		case <-current:
			return
		case <-tick:
			log.Warn("still waiting for current formation list", "duration", time.Since(start))
		}
	}
}

func (s *Scheduler) streamHostEvents() {
	log := s.logger.New("fn", "streamHostEvents")

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

	current := make(chan struct{})
	go func() {
		var isCurrent bool
		for {
			for {
				if err := connect(); err == nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			for event := range events {
				switch event.Kind {
				case discoverd.EventKindCurrent:
					if !isCurrent {
						isCurrent = true
						close(current)
					}
				case discoverd.EventKindUp, discoverd.EventKindUpdate, discoverd.EventKindDown:
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
		}
	}()

	// wait until we have the current list of hosts and their
	// jobs before starting the main scheduler loop
	start := time.Now()
	tick := time.Tick(30 * time.Second)
	for {
		select {
		case <-current:
			return
		case <-tick:
			log.Warn("still waiting for current host and job list", "duration", time.Since(start))
		}
	}
}

func (s *Scheduler) streamVolumeEvents() {
	log := s.logger.New("fn", "streamVolumeEvents")

	var events chan *ct.Volume
	var stream stream.Stream
	var since *time.Time
	connect := func() (err error) {
		log.Info("connecting volume event stream")
		events = make(chan *ct.Volume, eventBufferSize)
		stream, err = s.StreamVolumes(since, events)
		if err != nil {
			log.Error("error connecting volume event stream", "err", err)
		}
		return
	}

	current := make(chan struct{})
	go func() {
		var isCurrent bool
		for {
			for {
				if err := connect(); err == nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			for volume := range events {
				// a volume without an ID indicates we now have
				// the current list of volumes.
				if volume.ID == "" {
					if !isCurrent {
						isCurrent = true
						close(current)
					}
					continue
				}
				event := &VolumeEvent{
					Type:   VolumeEventTypeController,
					Volume: &Volume{Volume: *volume},
				}
				since = volume.UpdatedAt
				// if we are not current, explicitly handle the event
				// so that the scheduler has the current list of
				// volumes before starting the main loop.
				if !isCurrent {
					s.HandleVolumeEvent(event)
					continue
				}
				s.volumeEvents <- event
			}
			log.Warn("volume event stream disconnected", "err", stream.Err())
		}
	}()

	// wait until we have the current list of volumes before
	// starting the main scheduler loop
	start := time.Now()
	tick := time.Tick(30 * time.Second)
	for {
		select {
		case <-current:
			return
		case <-tick:
			log.Warn("still waiting for current volume list", "duration", time.Since(start))
		}
	}
}

func (s *Scheduler) streamRouterEvents() {
	log := s.logger.New("fn", "streamRouterEvents")

	var events chan *discoverd.Event
	var stream stream.Stream
	connect := func() (err error) {
		log.Info("connecting router event stream")
		events = make(chan *discoverd.Event, eventBufferSize)
		stream, err = discoverd.NewService("router-api").Watch(events)
		if err != nil {
			log.Error("error connecting router event stream", "err", err)
		}
		return
	}
	for {
		for {
			if err := connect(); err == nil {
				break
			}
			time.Sleep(time.Second)
		}
		for event := range events {
			s.routerServiceEvents <- event
		}
		log.Warn("router event stream disconnected", "err", stream.Err())
	}
}

func (s *Scheduler) streamSinkEvents() error {
	log := s.logger.New("fn", "streamSinkEvents")

	var events chan *ct.Sink
	var stream stream.Stream
	var since *time.Time
	connect := func() (err error) {
		log.Info("connecting log sink event stream")
		events = make(chan *ct.Sink, eventBufferSize)
		stream, err = s.StreamSinks(since, events)
		if err != nil {
			log.Error("error connecting log sink event stream", "err", err)
		}
		return
	}
	for {
		for {
			if err := connect(); err == nil {
				break
			}
			time.Sleep(time.Second)
		}
		for event := range events {
			if event.ID == "" {
				continue
			}
			s.sinkEvents <- event
		}
		log.Warn("log sink event stream disconnected", "err", stream.Err())
	}
}

func (s *Scheduler) Run() {
	log := s.logger.New("fn", "Run")
	log.Info("starting scheduler loop")
	defer log.Info("scheduler loop exited")

	go s.ControllerPersistLoop()
	defer close(s.controllerPersist)

	// stream host events (which will start watching job events on
	// all current hosts before returning) *before* registering in
	// service discovery so that there is always at least one scheduler
	// watching all job events, even during a deployment.
	s.streamHostEvents()

	s.streamFormationEvents()

	// ensure we have the current list of volumes before starting the main
	// loop so we don't schedule any jobs using decommissioned volumes
	s.streamVolumeEvents()

	isLeader := s.discoverd.Register()
	s.HandleLeaderChange(isLeader)
	leaderCh := s.discoverd.LeaderCh()

	go s.streamRouterEvents()
	go s.streamSinkEvents()

	s.tickSyncJobs(30 * time.Second)
	s.tickSyncFormations(time.Minute)
	s.tickSyncSinks(time.Minute)
	s.tickSyncVolumes(time.Minute)
	s.tickSyncHosts(10 * time.Second)
	s.tickSendTelemetry()

	for {
		select {
		case <-s.stop:
			log.Info("stopping scheduler loop")
			return
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
		case e := <-s.serviceEvents:
			s.HandleServiceEvent(e)
			continue
		case e := <-s.routerServiceEvents:
			s.HandleRouterServiceEvent(e)
			continue
		case e := <-s.routerStreamEvents:
			s.HandleRouterStreamEvent(e)
			continue
		case <-s.hostChecks:
			s.PerformHostChecks()
			continue
		case e := <-s.jobEvents:
			s.HandleJobEvent(e)
			continue
		case e := <-s.volumeEvents:
			s.HandleVolumeEvent(e)
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
		case <-s.syncSinks:
			s.SyncSinks()
			continue
		case <-s.syncJobs:
			s.SyncJobs()
			continue
		case <-s.syncHosts:
			s.SyncHosts()
			continue
		case <-s.syncVolumes:
			s.SyncVolumes()
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
			return
		case isLeader := <-leaderCh:
			s.HandleLeaderChange(isLeader)
		case req := <-s.placementRequests:
			s.HandlePlacementRequest(req)
		case req := <-s.internalStateRequests:
			s.HandleInternalStateRequest(req)
		case e := <-s.hostEvents:
			s.HandleHostEvent(e)
		case e := <-s.serviceEvents:
			s.HandleServiceEvent(e)
		case e := <-s.routerServiceEvents:
			s.HandleRouterServiceEvent(e)
		case e := <-s.routerStreamEvents:
			s.HandleRouterStreamEvent(e)
		case <-s.hostChecks:
			s.PerformHostChecks()
		case e := <-s.jobEvents:
			s.HandleJobEvent(e)
		case e := <-s.volumeEvents:
			s.HandleVolumeEvent(e)
		case f := <-s.formationEvents:
			s.HandleFormationChange(f)
		case e := <-s.sinkEvents:
			s.HandleSinkChange(e)
		case <-s.syncFormations:
			s.SyncFormations()
		case <-s.syncJobs:
			s.SyncJobs()
		case <-s.syncHosts:
			s.SyncHosts()
		case <-s.syncVolumes:
			s.SyncVolumes()
		case <-s.sendTelemetry:
			s.SendTelemetry()
		case <-s.syncSinks:
			s.SyncSinks()
		case <-s.pause:
			<-s.resume
		}
	}
}

func (s *Scheduler) IsLeader() bool {
	return s.isLeader != nil && *s.isLeader
}

func (s *Scheduler) SyncJobs() (err error) {
	log := s.logger.New("fn", "SyncJobs")
	log.Info("syncing jobs")

	defer func() {
		if err != nil {
			// try again soon
			time.AfterFunc(100*time.Millisecond, s.triggerSyncJobs)
		}
	}()

	// ensure we have accurate in-memory states for all active jobs
	activeHostJobs := map[string]struct{}{}
	for id, host := range s.hosts {
		jobs, err := host.client.ListActiveJobs()
		if err != nil {
			log.Error("error getting active host jobs", "host.id", id, "err", err)
			return err
		}

		for _, job := range jobs {
			activeHostJobs[job.Job.ID] = struct{}{}
			s.handleActiveJob(&job)
		}
	}

	// ensure any jobs we think are active are still active (in case we
	// missed the event which marked the job as down)
	for _, job := range s.jobs {
		if job.State != JobStateStarting && job.State != JobStateRunning {
			continue
		}
		if _, ok := activeHostJobs[job.JobID]; ok {
			continue
		}
		log.Warn("job no longer active", "job.id", job.JobID, "job.state", job.State)
		// get the job from the host to determine why it is no longer active
		host, ok := s.hosts[job.HostID]
		if !ok {
			// assume the job doesn't exist, mark it as stopped
			log.Error("error getting inactive job, unknown host", "job.id", job.JobID, "host.id", job.HostID)
			s.markAsStopped(job)
			continue
		}
		j, err := host.client.GetJob(job.JobID)
		if err != nil {
			// assume the job doesn't exist, mark it as stopped
			log.Error("error getting inactive job, marking as stopped", "job.id", job.JobID, "err", err)
			s.markAsStopped(job)
			continue
		}
		s.handleActiveJob(j)
	}

	// ensure that all pending / starting / up jobs in the controller are
	// still in those states
	jobs, err := s.JobListActive()
	if err != nil {
		if err == controller.ErrNotFound {
			// a 404 means the controller is a version behind the scheduler (which
			// can happen during an update), just ignore and wait for the next sync
			// when the controller may be updated to the correct version
			log.Warn("skipping controller job sync, controller missing active job route")
			return nil
		}
		log.Error("error getting controller active jobs", "err", err)
		return err
	}
	activeControllerJobs := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		activeControllerJobs[job.UUID] = struct{}{}

		j, ok := s.jobs[job.UUID]
		if !ok {
			// the controller job is unknown, and since we are in sync with
			// all the hosts, it can't be running so mark it as down
			job.State = ct.JobStateDown
			s.persistControllerJob(job)
			continue
		}

		// ignore jobs in the JobStateStopping state since, although a
		// request has been made to stop the job, we don't yet know if
		// it has actually stopped, so just leave it in whatever state
		// it's currently in until we get the stopped event
		if j.State == JobStateStopping {
			continue
		}

		// persist the job if it has a different in-memory state
		if job.State == ct.JobStatePending && j.State != JobStatePending ||
			job.State == ct.JobStateStarting && j.State != JobStateStarting ||
			job.State == ct.JobStateUp && j.State != JobStateRunning ||
			job.State == ct.JobStateStopping {
			s.persistJob(j)
		}
	}

	for _, job := range s.jobs {
		switch job.State {

		// ensure any active in-memory jobs are also active in the
		// controller (the two may diverge if for example a previous
		// persistence of the job's state failed with a non-retryable
		// error)
		case JobStatePending, JobStateStarting, JobStateRunning:
			if _, active := activeControllerJobs[job.ID]; !active {
				s.persistJob(job)
			}

		// stop any jobs in the JobStateStopping state (this races with
		// an already running stopJob call, but stopping a job multiple
		// times isn't a big deal and avoids the need for synchronization)
		case JobStateStopping:
			s.stopJob(job)
		}
	}

	return nil
}

func (s *Scheduler) SyncFormations() {
	log := s.logger.New("fn", "SyncFormations")
	log.Info("syncing formations")
	defer log.Debug("formations synced")

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

func (s *Scheduler) SyncSinks() {
	log := s.logger.New("fn", "SyncSinks")
	log.Info("syncing log sinks")

	sinks, err := s.ListSinks()
	if err != nil {
		log.Error("error getting controller sinks", "err", err)
		return
	}

	active := make(map[string]struct{}, len(sinks))
	for _, sink := range sinks {
		active[sink.ID] = struct{}{}
		s.handleSink(sink)
	}

	// check that all sinks we think are active are still active
	for _, sink := range s.sinks {
		if _, ok := active[sink.ID]; !ok {
			log.Warn("sink should no longer be active, removing", "sink.id", sink.ID)
			sink.Config = nil
			s.handleSink(sink)
		}
	}

	// make sure all hosts have the correct sinks
	for _, host := range s.hosts {
		sinks, err := host.GetSinks()
		if err != nil {
			log.Error("error getting host sinks", "host.id", host.ID, "err", err)
			continue
		}

		for _, sink := range sinks {
			if sink.HostManaged {
				continue // Ignore sinks managed by the host
			}
			expected, ok := s.sinks[sink.ID]
			if !ok {
				log.Warn("removing non existent host sink", "host.id", host.ID, "sink.id", sink.ID)
				host.RemoveSink(sink.ID)
			} else if !reflect.DeepEqual(sink.Config, expected.Config) {
				log.Warn("updating stale host sink", "host.id", host.ID, "sink.id", sink.ID)
				host.AddSink(expected)
			}
		}
	}
}

func (s *Scheduler) SyncVolumes() {
	log := s.logger.New("fn", "SyncVolumes")
	log.Info("syncing volumes")

	// ensure we know about all existing app volumes
	for _, host := range s.hosts {
		volumes, err := host.client.ListVolumes()
		if err != nil {
			log.Error("error getting host volumes", "host.id", host.ID, "err", err)
			return
		}
		for _, info := range volumes {
			if !isAppVolume(info) {
				continue
			}
			if _, ok := s.volumes[info.ID]; !ok {
				vol := NewVolume(info, ct.VolumeStateCreated, host.ID)
				s.volumes[info.ID] = vol
				s.persistVolume(vol)
			}
		}
	}

	// ensure we know about all decommissioned volumes
	volumes, err := s.VolumeList()
	if err != nil {
		log.Error("error getting controller volumes", "err", err)
		return
	}
	for _, volume := range volumes {
		s.handleControllerVolume(volume)
	}
}

func (s *Scheduler) SyncHosts() (err error) {
	log := s.logger.New("fn", "SyncHosts")
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

		h, err := s.followHost(host)
		if err == nil {
			// make sure no jobs are blocked which needn't be
			s.maybeStartBlockedJobs(h)
		} else {
			log.Error("error following host", "host.id", host.ID(), "err", err)
			// finish the sync before returning the error
			followErr = err
		}
	}

	// mark any hosts as unhealthy which are not returned from s.Hosts()
	// and are not explicitly shutdown
	for id, host := range s.hosts {
		if _, ok := known[id]; !ok && !host.Shutdown {
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
	if !s.IsLeader() {
		return
	}
	defer s.logger.New("fn", "RectifyFormation").Debug("rectified formation", "key", key)

	formation, ok := s.formations[key]
	if !ok {
		return
	}

	// stop surplus omni jobs first in case we need to reschedule them
	// on other hosts
	s.stopSurplusOmniJobs(formation)

	// stop jobs with mismatched tags first in case we need to reschedule
	// them on hosts with matching tags
	s.stopJobsWithMismatchedTags(formation)

	// if there is a pending scale request, mark it as complete if the
	// formation has the correct number of running jobs
	if req := formation.PendingScaleRequest; req != nil && req.State == ct.ScaleRequestStatePending {
		running := make(Processes)
		for _, job := range s.jobs {
			if job.IsInFormation(key) && (job.State == JobStateRunning || job.State == JobStateStopping) {
				running[job.Type]++
			}
		}
		if running.Equals(formation.GetProcesses()) {
			s.markScaleRequestAsComplete(req)
		}
	}

	diff := s.formationDiff(formation)
	if diff.IsEmpty() {
		return
	}
	s.handleFormationDiff(formation, diff)
}

func (s *Scheduler) markScaleRequestAsComplete(req *ct.ScaleRequest) {
	s.logger.Info("marking scale request as complete", "scale_request.id", req.ID, "app.id", req.AppID, "release.id", req.ReleaseID)
	req.State = ct.ScaleRequestStateComplete
	s.controllerPersist <- req
}

// stopSurplusOmniJobs stops surplus jobs which are running on a host which has
// more than the expected number of omni jobs for a given type (this happens
// for example when both the bootstrapper and scheduler are starting jobs and
// distribute omni jobs unevenly)
func (s *Scheduler) stopSurplusOmniJobs(formation *Formation) {
	log := s.logger.New("fn", "stopSurplusOmniJobs")

	for typ, proc := range formation.Release.Processes {
		if !proc.Omni {
			continue
		}

		// get a list of jobs per host so we can count them and
		// potentially stop any surplus ones
		hostJobs := make(map[string][]*Job)
		for _, job := range s.jobs.WithFormationAndType(formation, typ) {
			if job.IsRunning() {
				hostJobs[job.HostID] = append(hostJobs[job.HostID], job)
			}
		}

		// detect surplus jobs per host by comparing the expected count
		// from the formation with the number of jobs currently running
		// on that host
		expected := formation.OriginalProcesses[typ]
		var surplusJobs []*Job
		for _, jobs := range hostJobs {
			if diff := len(jobs) - expected; diff > 0 {
				// add the most recent jobs which are at the start
				// of the slice (WithFormationAndType returns them
				// in reverse chronological order above)
				surplusJobs = append(surplusJobs, jobs[0:diff]...)
			}
		}

		if len(surplusJobs) == 0 {
			continue
		}

		log.Info(fmt.Sprintf("detected %d surplus omni jobs", len(surplusJobs)), "type", typ)
		for _, job := range surplusJobs {
			s.stopJob(job)
		}
	}
}

// stopJobsWithMismatchedTags stops any running jobs whose tags do not match
// those of the host they are running on (possible after either the host's tags
// or the formation's tags are updated)
func (s *Scheduler) stopJobsWithMismatchedTags(formation *Formation) {
	log := s.logger.New("fn", "stopJobsWithMismatchedTags")
	for _, job := range s.jobs {
		if !job.IsInFormation(formation.key()) || !job.IsRunning() {
			continue
		}
		host, ok := s.hosts[job.HostID]
		if !ok {
			continue
		}
		if job.TagsMatchHost(host) {
			continue
		}
		log.Info("job has mismatched tags, stopping", "job.id", job.ID, "job.tags", job.Tags(), "host.id", host.ID, "host.tags", host.Tags)
		s.stopJob(job)
	}
}

// maybeStartBlockedJobs starts any jobs which are blocked due to not
// matching tags of any hosts on the given host, which is expected to be
// either a new host or a host whose tags have just changed
func (s *Scheduler) maybeStartBlockedJobs(host *Host) {
	for _, job := range s.jobs {
		if job.State == JobStateBlocked && job.TagsMatchHost(host) {
			job.State = JobStatePending
			go s.StartJob(job)
		}
	}
}

func (s *Scheduler) formationDiff(formation *Formation) Processes {
	if formation == nil {
		return nil
	}
	key := formation.key()
	actual := s.jobs.GetProcesses(key)
	diff := formation.Diff(actual)
	if !diff.IsEmpty() {
		log := s.logger.New("fn", "formationDiff", "app.id", key.AppID, "release.id", key.ReleaseID)
		log.Info("expected different from actual", "expected", formation.Processes, "actual", actual, "diff", diff)
	}
	return diff
}

func (s *Scheduler) HandleFormationChange(ef *ct.ExpandedFormation) {
	log := s.logger.New("fn", "HandleFormationChange", "app.id", ef.App.ID, "release.id", ef.Release.ID, "processes", ef.Processes)
	log.Info("handling formation change")
	defer log.Debug("formation change handled")
	s.handleFormation(ef)
}

func (s *Scheduler) HandleSinkChange(sink *ct.Sink) {
	s.logger.Info("handling sink change", "sink.id", sink.ID)
	s.handleSink(sink)
}

// findVolume looks for an existing, unassigned volume which matches the given
// job's app, release and type, and the volume request's path
func (s *Scheduler) findVolume(job *Job, req *ct.VolumeReq) *Volume {
	for _, vol := range s.volumes {
		// skip destroyed or decommissioned volumes
		if vol.GetState() == ct.VolumeStateDestroyed || vol.DecommissionedAt != nil {
			continue
		}

		// skip if the app, release, type or path do not match
		if vol.AppID != job.AppID {
			continue
		}
		if vol.ReleaseID != job.ReleaseID {
			continue
		}
		if vol.JobType != job.Type {
			continue
		}
		if vol.Path != req.Path {
			continue
		}

		// skip if the volume is assigned to another job
		if vol.JobID != nil && *vol.JobID != job.ID {
			continue
		}

		// skip if we have already assigned the job to a host
		// and this volume doesn't exist on that host
		if job.HostID != "" && vol.HostID != job.HostID {
			continue
		}

		// return the matching volume
		return vol
	}
	return nil
}

func (s *Scheduler) HandlePlacementRequest(req *PlacementRequest) {
	if !s.IsLeader() {
		req.Error(ErrNotLeader)
		return
	}

	// don't attempt to place a job which is no longer pending, which could
	// be the case either if the job has been marked as stopped, or AddJob
	// failed in some way (e.g. a timeout) but the job did actually start
	if req.Job.State != JobStatePending {
		req.Error(ErrJobNotPending)
		return
	}

	log := s.logger.New("fn", "HandlePlacementRequest", "app.id", req.Job.AppID, "release.id", req.Job.ReleaseID, "job.type", req.Job.Type, "job.tags", req.Job.Tags())
	log.Info("handling placement request")

	if len(s.hosts) == 0 {
		req.Error(ErrNoHosts)
		return
	}

	// reset the job's HostID in case we already placed it but it failed to
	// start
	req.Job.HostID = ""

	// if the job has volume requests, assign existing volumes if
	// possible (which will lead to the job being scheduled on the
	// same host as the volumes) or initialize new ones
	if reqs := req.Job.VolumeRequests(); len(reqs) > 0 {
		req.Job.Volumes = make([]*Volume, len(reqs))

		for i, volReq := range reqs {
			// look for an existing, unassigned volume
			vol := s.findVolume(req.Job, &volReq)

			if vol == nil {
				// did not assign an existing volume so initialize a new one
				// (the StartJob goroutine will actually create the volume
				// on the selected host)
				vol = &Volume{
					Volume: ct.Volume{
						VolumeReq: volReq,
						ID:        random.UUID(),
						Type:      volume.VolumeTypeData,
						State:     ct.VolumeStatePending,
						AppID:     req.Job.AppID,
						ReleaseID: req.Job.ReleaseID,
						JobID:     &req.Job.ID,
						JobType:   req.Job.Type,
						Meta: map[string]string{
							"flynn-controller.app":            req.Job.AppID,
							"flynn-controller.release":        req.Job.ReleaseID,
							"flynn-controller.type":           req.Job.Type,
							"flynn-controller.path":           volReq.Path,
							"flynn-controller.delete_on_stop": strconv.FormatBool(volReq.DeleteOnStop),
						},
					},
				}
				s.volumes[vol.ID] = vol
			}

			// if we picked a volume that exists on a host which
			// either doesn't match the job's tags or is down,
			// abort the placement and leave the job blocked until
			// either an operator decommissions the volume or the
			// host comes back up
			if vol.HostID != "" {
				host, ok := s.hosts[vol.HostID]
				if !ok {
					req.Job.State = JobStateBlocked
					s.persistJob(req.Job)
					req.Error(ErrHostIsDown)
					return
				} else if !req.Job.TagsMatchHost(host) {
					req.Job.State = JobStateBlocked
					s.persistJob(req.Job)
					req.Error(ErrNoHostsMatchTags)
					return
				}
				req.Host = host
			}

			log.Info("assigning volume", "vol.id", vol.ID, "vol.path", vol.Path)
			vol.JobID = &req.Job.ID
			req.Job.Volumes[i] = vol
		}

		if req.Host != nil {
			log.Info(fmt.Sprintf("placed job on host with existing %s volumes", req.Job.Type), "host.id", req.Host.ID)
		}
	}

	// if we didn't pick a host for the job's volumes, pick a host with
	// the least amount of jobs running of the given type
	if req.Host == nil {
		formation := req.Job.Formation
		counts := s.jobs.GetHostJobCounts(formation.key(), req.Job.Type)
		var minCount int = math.MaxInt32
		for _, h := range s.ShuffledHosts() {
			if h.Shutdown {
				continue
			}
			if !req.Job.TagsMatchHost(h) {
				continue
			}
			count, ok := counts[h.ID]
			if !ok || count == 0 {
				req.Host = h
				break
			}
			if count < minCount {
				minCount = count
				req.Host = h
			}
		}

		// if we still didn't pick a host, the job's tags don't match
		// any hosts so mark it as blocked and return an error to
		// cause the StartJob goroutine to stop trying to place the job
		if req.Host == nil {
			req.Job.State = JobStateBlocked
			s.persistJob(req.Job)
			req.Error(ErrNoHostsMatchTags)
			return
		}

		if len(req.Job.Tags()) == 0 {
			log.Info(fmt.Sprintf("placed job on host with least %s jobs", req.Job.Type), "host.id", req.Host.ID)
		} else {
			log.Info(fmt.Sprintf("placed job on host with matching tags and least %s jobs", req.Job.Type), "host.id", req.Host.ID, "host.tags", req.Host.Tags)
		}
	}

	req.Config = jobConfig(req.Job, req.Host.ID)
	req.Job.JobID = req.Config.ID
	req.Job.HostID = req.Host.ID
	for _, vol := range req.Job.Volumes {
		if vol.HostID == "" {
			vol.HostID = req.Host.ID
		}
	}
	req.Error(nil)
}

type InternalState struct {
	JobID      string                `json:"job_id"`
	Hosts      map[string]*Host      `json:"hosts"`
	Jobs       Jobs                  `json:"jobs"`
	Formations map[string]*Formation `json:"formations"`
	Volumes    map[string]*Volume    `json:"volumes"`
	IsLeader   *bool                 `json:"is_leader,omitempty"`
}

func NewInternalStateRequest() *InternalStateRequest {
	return &InternalStateRequest{Done: make(chan struct{})}
}

type InternalStateRequest struct {
	State *InternalState
	Done  chan struct{}
}

func (s *Scheduler) HandleInternalStateRequest(req *InternalStateRequest) {
	log := s.logger.New("fn", "HandleInternalStateRequest")
	log.Info("handling internal state request")

	// create an InternalState as a snapshot of the current state by
	// copying objects and their exported fields
	req.State = &InternalState{
		JobID:      os.Getenv("FLYNN_JOB_ID"),
		Hosts:      make(map[string]*Host, len(s.hosts)),
		Jobs:       make(map[string]*Job, len(s.jobs)),
		Formations: make(map[string]*Formation, len(s.formations)),
		Volumes:    make(map[string]*Volume, len(s.volumes)),
		IsLeader:   s.isLeader,
	}

	for id, host := range s.hosts {
		h := *host
		h.Tags = make(map[string]string, len(host.Tags))
		for key, val := range host.Tags {
			h.Tags[key] = val
		}
		req.State.Hosts[id] = &h
	}

	for id, job := range s.jobs {
		req.State.Jobs[id] = &(*job)
	}

	for key, formation := range s.formations {
		f := Formation{
			ExpandedFormation: &ct.ExpandedFormation{
				App: &ct.App{
					ID:   formation.App.ID,
					Name: formation.App.Name,
					Meta: formation.App.Meta,
				},
				Release: &ct.Release{
					ID:        formation.Release.ID,
					Processes: make(map[string]ct.ProcessType, len(formation.Release.Processes)),
				},
				Processes: make(map[string]int, len(formation.Processes)),
				Tags:      make(map[string]map[string]string, len(formation.Tags)),
				UpdatedAt: formation.UpdatedAt,
			},
			OriginalProcesses: formation.OriginalProcesses,
		}
		for typ, n := range formation.Processes {
			f.Processes[typ] = n
		}
		for typ, n := range formation.OriginalProcesses {
			f.OriginalProcesses[typ] = n
		}
		for typ, tags := range formation.Tags {
			f.Tags[typ] = make(map[string]string, len(tags))
			for key, val := range tags {
				f.Tags[typ][key] = val
			}
		}
		for name, proc := range formation.Release.Processes {
			f.Release.Processes[name] = ct.ProcessType{
				Args:    proc.Args,
				Volumes: proc.Volumes,
				Omni:    proc.Omni,
			}
		}
		req.State.Formations[key.String()] = &f
	}

	for id, vol := range s.volumes {
		req.State.Volumes[id] = &(*vol)
	}

	close(req.Done)
}

func (s *Scheduler) InternalState() *InternalState {
	req := NewInternalStateRequest()
	s.internalStateRequests <- req
	<-req.Done
	return req.State
}

func (s *Scheduler) ShuffledHosts() []*Host {
	hosts := make([]*Host, 0, len(s.hosts))
	for _, host := range s.hosts {
		hosts = append(hosts, host)
	}
	for i := len(hosts) - 1; i > 0; i-- {
		j := random.Math.Intn(i + 1)
		hosts[i], hosts[j] = hosts[j], hosts[i]
	}
	return hosts
}

var controllerPersistAttempts = attempt.Strategy{
	Delay: 100 * time.Millisecond,
	Total: time.Minute,
}

// ControllerPersistLoop starts a loop which receives jobs, volumes and scale
// requests from the s.controllerPersist channel and persists them to the
// controller using the controllerPersistAttempts retry strategy.
//
// A goroutine is started per job, volume and scale request to persist, but
// care is taken to persist jobs with the same UUID sequentially and in order
// (to avoid for example a job transitioning from "down" to "up" in the
// controller) and scale requests after associated jobs (so that scale events
// are emitted after job events).
func (s *Scheduler) ControllerPersistLoop() {
	log := s.logger.New("fn", "ControllerPersistLoop")
	log.Info("starting controller persistence loop")

	// jobQueue is a map of job UUID to a slice of jobs to persist for that
	// given UUID, and the loop below persists the jobs in the slice in
	// FIFO order
	jobQueue := make(map[string][]*ct.Job)

	// jobDone is a channel which receives a UUID once a job has been
	// persisted for that UUID, thus potentially triggering the
	// persistence of the next job in the queue for that UUID
	jobDone := make(chan string)

	// volQueue is a map of volume ID to a slice of volumes to persist for
	// that given ID, and the loop below persists the volumes in the slice
	// in FIFO order
	volQueue := make(map[string][]*ct.Volume)

	// volDone is a channel which receives an ID once a volume has been
	// persisted for that ID, thus potentially triggering the persistence
	// of the next volume in the queue for that ID
	volDone := make(chan string)

	// scaleRequests is a queue of scale requests waiting to be persisted
	// after the associated job events
	scaleRequests := make(map[string]*ct.ScaleRequest)

	// persistJob makes multiple attempts to persist the given job, sending
	// to the jobDone channel once the attempts have finished
	persistJob := func(job *ct.Job) {
		err := controllerPersistAttempts.RunWithValidator(func() error {
			return s.PutJob(job)
		}, httphelper.IsRetryableError)
		if err != nil {
			log.Error("error persisting job", "job.id", job.ID, "job.state", job.State, "err", err)
		}
		jobDone <- job.UUID
	}

	// persistVolume makes multiple attempts to persist the given volume,
	// sending to the volDone channel once the attempts have finished
	persistVolume := func(vol *ct.Volume) {
		err := controllerPersistAttempts.RunWithValidator(func() error {
			return s.PutVolume(vol)
		}, httphelper.IsRetryableError)
		if err != nil {
			s.logger.Error("error persisting volume", "vol.id", vol.ID, "vol.state", vol.State, "err", err)
		}
		volDone <- vol.ID
	}

	maybePersistScaleRequest := func(req *ct.ScaleRequest) {
		// if there are any associated jobs being persisted, add to the
		// scale request queue to be persisted later (to avoid scale
		// events preceding job events)
		for _, jobs := range jobQueue {
			for _, job := range jobs {
				if job.AppID == req.AppID && job.ReleaseID == req.ReleaseID {
					scaleRequests[req.ID] = req
					return
				}
			}
		}

		go func() {
			err := controllerPersistAttempts.RunWithValidator(func() error {
				return s.PutScaleRequest(req)
			}, httphelper.IsRetryableError)
			if err != nil {
				log.Error("error marking scale request as complete",
					"scale_request.id", req.ID, "app.id", req.AppID, "release.id", req.ReleaseID, "err", err)
			}
		}()
		delete(scaleRequests, req.ID)
	}

	// start the persistence loop which receives from s.controllerPersist,
	// jobDone and volDone, modifies the queues accordingly and then calls
	// the persist functions if necessary
	for {
		select {
		case v, ok := <-s.controllerPersist:
			if !ok {
				log.Info("stopping controller persistence loop")
				return
			}

			switch v := v.(type) {
			case *ct.Job:
				// push the job to the back of the queue
				jobQueue[v.UUID] = append(jobQueue[v.UUID], v)

				// if there is only one job in the queue, persist it
				if len(jobQueue[v.UUID]) == 1 {
					go persistJob(v)
				}
			case *ct.Volume:
				// push the volume to the back of the queue
				volQueue[v.ID] = append(volQueue[v.ID], v)

				// if there is only one volume in the queue, persist it
				if len(volQueue[v.ID]) == 1 {
					go persistVolume(v)
				}
			case *ct.ScaleRequest:
				maybePersistScaleRequest(v)
			}
		case uuid := <-jobDone:
			// remove the persisted job from the queue
			jobQueue[uuid] = jobQueue[uuid][1:]

			// if the queue has more jobs, persist the first one
			if len(jobQueue[uuid]) > 0 {
				go persistJob(jobQueue[uuid][0])
			} else {
				delete(jobQueue, uuid)
			}

			// try and persist scale requests
			for _, req := range scaleRequests {
				maybePersistScaleRequest(req)
			}
		case id := <-volDone:
			// remove the persisted volume from the queue
			volQueue[id] = volQueue[id][1:]

			// if the queue has more volumes, persist the first one
			if len(volQueue[id]) > 0 {
				go persistVolume(volQueue[id][0])
			} else {
				delete(volQueue, id)
			}
		}
	}
}

func (s *Scheduler) HandleLeaderChange(isLeader bool) {
	log := s.logger.New("fn", "HandleLeaderChange")
	s.isLeader = &isLeader
	if isLeader {
		log.Info("handling leader promotion")
		// ensure we are in sync and then rectify
		s.SyncHosts()
		s.SyncFormations()
		s.SyncJobs()
		s.rectifyAll()
		s.triggerSendTelemetry()
	} else {
		log.Info("handling leader demotion")
	}
}

func (s *Scheduler) handleFormationDiff(f *Formation, diff Processes) {
	log := s.logger.New("fn", "handleFormationDiff", "app.id", f.App.ID, "release.id", f.Release.ID)
	log.Info("formation in incorrect state", "diff", diff)
	for typ, n := range diff {
		if n > 0 {
			log.Info(fmt.Sprintf("starting %d new %s jobs", n, typ))
			for i := 0; i < n; i++ {
				job := &Job{
					ID:        s.generateJobUUID(),
					Type:      typ,
					AppID:     f.App.ID,
					ReleaseID: f.Release.ID,
					Formation: f,
					StartedAt: time.Now(),
					State:     JobStatePending,
					Args:      f.Release.Processes[typ].Args,
				}
				s.jobs.Add(job)

				// persist the job so that it appears as pending in the database
				s.persistJob(job)

				go s.StartJob(job)
			}
		} else if n < 0 {
			log.Info(fmt.Sprintf("stopping %d %s jobs", -n, typ))
			for i := 0; i < -n; i++ {
				s.stopJobOfType(f, typ)
			}
		}
	}
}

// activeFormationCount returns the number of formations which have running
// jobs for the given app
func (s *Scheduler) activeFormationCount(appID string) int {
	activeReleases := make(map[string]struct{})
	for _, job := range s.jobs {
		if job.IsRunning() && job.IsInApp(appID) {
			activeReleases[job.Formation.Release.ID] = struct{}{}
		}
	}
	return len(activeReleases)
}

func (s *Scheduler) StartJob(job *Job) {
	log := s.logger.New("fn", "StartJob", "app.id", job.AppID, "release.id", job.ReleaseID, "job.id", job.ID, "job.type", job.Type)
	log.Info("starting job")

outer:
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			// when making multiple attempts, backoff in increments
			// of 500ms (capped at 30s)
			delay := 500 * time.Millisecond * time.Duration(attempt)
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			log.Warn(fmt.Sprintf("waiting %s before re-attempting job placement", delay))
			time.Sleep(delay)
		}

		log.Info("placing job in the cluster")
		req, err := s.PlaceJob(job)
		if err == ErrNotLeader {
			log.Warn("not starting job as not leader")
			return
		} else if err == ErrNoHostsMatchTags {
			log.Warn("unable to place job as tags don't match any hosts")
			return
		} else if err == ErrJobNotPending {
			log.Warn("unable to place job as it is no longer pending")
			return
		} else if err == ErrHostIsDown {
			log.Warn("unable to place job as the host is down")
			return
		} else if err != nil {
			log.Error("error placing job in the cluster", "err", err)
			continue
		}

		for _, vol := range job.Volumes {
			if vol.GetState() == ct.VolumeStatePending {
				log.Info("creating new volume", "host.id", req.Host.ID, "vol.id", vol.ID, "vol.path", vol.Path)
				if err := req.Host.client.CreateVolume("default", vol.Info()); err != nil {
					log.Error("error creating new volume", "vol.id", vol.ID, "err", err)
					continue outer
				}
				vol.SetState(ct.VolumeStateCreated)
				s.persistVolume(vol)
			}
		}

		log.Info("adding job to the cluster", "host.id", req.Host.ID)
		err = req.Host.client.AddJob(req.Config)
		if err == nil {
			return
		}
		log.Error("error adding job to the cluster", "attempts", attempt+1, "err", err)
	}
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

func (s *Scheduler) PlaceJob(job *Job) (*PlacementRequest, error) {
	req := &PlacementRequest{
		Job: job,
		Err: make(chan error),
	}
	s.placementRequests <- req
	return req, <-req.Err
}

func (s *Scheduler) followHost(h utils.HostClient) (*Host, error) {
	if host, ok := s.hosts[h.ID()]; ok {
		return host, nil
	}

	host := NewHost(h, s.logger)
	volumes, err := host.StreamVolumeEventsTo(s.volumeEvents)
	if err != nil {
		return nil, err
	}
	for id, vol := range volumes {
		s.volumes[id] = vol
		s.persistVolume(vol)
	}
	jobs, err := host.StreamJobEventsTo(s.jobEvents)
	if err != nil {
		return nil, err
	}
	s.hosts[host.ID] = host

	for _, job := range jobs {
		s.handleActiveJob(&job)
	}

	s.triggerSyncFormations()

	return host, nil
}

func (s *Scheduler) unfollowHost(host *Host) {
	log := s.logger.New("fn", "unfollowHost", "host.id", host.ID)
	log.Info("unfollowing host")
	host.Close()
	delete(s.hosts, host.ID)

	// rectify the omni job counts so that when omni jobs are marked as
	// stopped, they are not restarted
	for _, formation := range s.formations {
		formation.RectifyOmni(s.activeHostCount())
	}

	for _, job := range s.jobs {
		if job.HostID == host.ID && job.State != JobStateStopped {
			log.Info("removing job", "job.id", job.JobID)
			s.markAsStopped(job)
		}
	}

	s.triggerSyncFormations()
}

func (s *Scheduler) markHostAsUnhealthy(host *Host) {
	s.logger.Warn("host service is down, marking as unhealthy and triggering host checks", "host.id", host.ID)
	host.Healthy = false
	s.triggerHostChecks()
}

func (s *Scheduler) HandleHostEvent(e *discoverd.Event) {
	log := s.logger.New("fn", "HandleHostEvent", "event.type", e.Kind)
	log.Info("handling host event")
	defer log.Debug("handled host event")

	switch e.Kind {
	case discoverd.EventKindUp:
		s.handleNewHost(e.Instance.Meta["id"])
	case discoverd.EventKindUpdate:
		id := e.Instance.Meta["id"]
		_, isShutdown := e.Instance.Meta["shutdown"]

		// if we haven't seen this host before, handle it as new
		// (provided it is not shutdown)
		host, ok := s.hosts[id]
		if !ok {
			if !isShutdown {
				s.handleNewHost(id)
			}
			return
		}

		// if the host is shutdown, just mark it as shutdown and return
		// rather than explicitly unfollowing to avoid a race where
		// SyncHosts could run before we get the down event, thus
		// re-following a host we know is shutdown (the host will be
		// unfollowed when we get the eventual down event)
		if isShutdown {
			log.Info("marking host as shutdown", "host.id", host.ID)
			host.Shutdown = true

			// rectify the omni job counts now the host is shutdown
			// so that when down events are received for omni jobs,
			// they are not restarted
			for _, formation := range s.formations {
				formation.RectifyOmni(s.activeHostCount())
			}

			return
		}

		// if the host's tags have changed, rectify all formations so
		// that any running jobs with mismatched tags are stopped, and
		// also try to start pending jobs in case tags now match
		tags := cluster.HostTagsFromMeta(e.Instance.Meta)
		if !host.TagsEqual(tags) {
			log.Info("host tags changed", "host.id", id, "from", host.Tags, "to", tags)
			host.Tags = tags
			s.rectifyAll()
			s.maybeStartBlockedJobs(host)
		}
	case discoverd.EventKindDown:
		id := e.Instance.Meta["id"]
		log = log.New("host.id", id)
		host, ok := s.hosts[id]
		if !ok {
			log.Warn("ignoring host down event, unknown host")
			return
		}
		if host.Shutdown {
			s.unfollowHost(host)
		} else {
			s.markHostAsUnhealthy(host)
		}
	}
}

func (s *Scheduler) HandleServiceEvent(e *discoverd.Event) {
	if e.Instance == nil {
		return
	}

	jobID := e.Instance.Meta["FLYNN_JOB_ID"]
	jobType := e.Instance.Meta["FLYNN_PROCESS_TYPE"]
	log := s.logger.New("fn", "HandleServiceEvent", "service", e.Service, "kind", e.Kind, "job.id", jobID, "job.type", jobType)
	log.Info("handling service event")

	id, err := cluster.ExtractUUID(jobID)
	if err != nil {
		log.Error("error handling service event, invalid job ID")
		return
	}

	job, ok := s.jobs[id]
	if !ok {
		log.Error("error handling service event, unknown job")
		return
	}

	if job.serviceFirstSeen == nil {
		now := time.Now()
		job.serviceFirstSeen = &now
	}

	if job.State != JobStatePending && job.State != JobStateStarting {
		return
	}

	shouldWaitForRouterBackends := func() bool {
		// don't wait if there are no routes for the service
		routes, ok := s.routes[job.Service()]
		if !ok {
			return false
		}

		// don't wait if the service was first seen more than
		// routerBackendUpTimeout ago
		if time.Since(*job.serviceFirstSeen) > routerBackendUpTimeout {
			return false
		}

		// don't wait if the job is up in all the routers which
		// have a route for the service
		if backend, ok := s.routerBackends[jobID]; ok && len(backend.Routers) >= len(routes) {
			return false
		}

		return true
	}

	if shouldWaitForRouterBackends() {
		log.Info("waiting for router backend events")
		time.AfterFunc(routerBackendUpTimeout, func() {
			s.serviceEvents <- e
		})
	} else {
		s.handleJobStatus(job, host.StatusRunning)
	}
}

func (s *Scheduler) HandleRouterServiceEvent(e *discoverd.Event) {
	switch e.Kind {
	case discoverd.EventKindUp, discoverd.EventKindUpdate:
		id := e.Instance.Meta["FLYNN_JOB_ID"]
		if _, ok := s.routers[id]; !ok {
			s.logger.Info("adding router", "router.id", id)
			s.routers[id] = NewRouter(id, e.Instance.Addr, s.routerStreamEvents, s.logger)
		}
	case discoverd.EventKindDown:
		id := e.Instance.Meta["FLYNN_JOB_ID"]
		if r, ok := s.routers[id]; ok {
			s.logger.Info("removing router", "router.id", id)
			r.Close()
			delete(s.routers, id)
		}
		for _, b := range s.routerBackends {
			s.HandleRouterStreamEvent(&RouterEvent{
				RouterID: id,
				Type:     router.EventTypeBackendDrained,
				Backend:  b.Backend,
			})
		}
	}
}

func (s *Scheduler) HandleRouterStreamEvent(e *RouterEvent) {
	log := s.logger.New("router.id", e.RouterID)
	if e.Backend != nil {
		log = log.New("job.id", e.Backend.JobID, "job.service", e.Backend.Service)
	}
	if e.Route != nil {
		log = log.New("route.service", e.Route.Service)
	}

	switch e.Type {
	case router.EventTypeRouteSet:
		routes, ok := s.routes[e.Route.Service]
		if !ok {
			routes = make(map[string]struct{})
			s.routes[e.Route.Service] = routes
		}
		routes[e.RouterID] = struct{}{}
		log.Info("route added", "router.count", len(routes))
	case router.EventTypeRouteRemove:
		routes, ok := s.routes[e.Route.Service]
		if !ok {
			return
		}
		delete(routes, e.RouterID)
		log.Info("route removed", "router.count", len(routes))
		if len(routes) == 0 {
			delete(s.routes, e.Route.Service)
		}
	case router.EventTypeBackendUp:
		backend, ok := s.routerBackends[e.Backend.JobID]
		if !ok {
			backend = NewRouterBackend(e.Backend)
			s.routerBackends[e.Backend.JobID] = backend
		}
		backend.Routers[e.RouterID] = struct{}{}
		log.Info("router backend is up", "router.count", len(backend.Routers))

		// if the backend is up in all the routers which have a route
		// for the service, mark it as running
		if len(backend.Routers) >= len(s.routes[e.Backend.Service]) {
			s.markRouterBackendUp(backend)
		}
	case router.EventTypeBackendDrained:
		backend, ok := s.routerBackends[e.Backend.JobID]
		if !ok {
			return
		}
		delete(backend.Routers, e.RouterID)
		log.Info("router backend has drained", "router.count", len(backend.Routers))
		if len(backend.Routers) == 0 {
			close(backend.Drained)
			delete(s.routerBackends, e.Backend.JobID)
		}
	}
}

func (s *Scheduler) markRouterBackendUp(b *RouterBackend) {
	id, err := cluster.ExtractUUID(b.Backend.JobID)
	if err != nil {
		return
	}
	job, ok := s.jobs[id]
	if !ok {
		return
	}
	if job.State == JobStatePending || job.State == JobStateStarting {
		s.handleJobStatus(job, host.StatusRunning)
	}
}

func (s *Scheduler) handleNewHost(id string) {
	log := s.logger.New("fn", "handleNewHost", "host.id", id)
	log.Info("host is up, starting job and volume event streams")
	h, err := s.Host(id)
	if err != nil {
		log.Error("error creating host client", "err", err)
		return
	}

	host, err := s.followHost(h)
	if err != nil {
		// just log the error, following will be retried in SyncHosts
		log.Error("error following host", "host.id", id, "err", err)
		return
	}

	// we have a new host which may now match the tags of some blocked jobs
	// and have their volumes so try to start them
	s.maybeStartBlockedJobs(host)
}

// activeHostCount returns the number of active hosts (i.e. all hosts which
// are not shutting down) and is used to determine how many omni jobs should
// be running when calling formation.RectifyOmni
func (s *Scheduler) activeHostCount() int {
	count := 0
	for _, host := range s.hosts {
		if !host.Shutdown {
			count++
		}
	}
	return count
}

func (s *Scheduler) PerformHostChecks() {
	log := s.logger.New("fn", "PerformHostChecks")
	log.Info("performing host checks")

	allHealthy := true

	for id, host := range s.hosts {
		if host.Healthy {
			continue
		}

		log := log.New("host.id", id)
		log.Info("getting status of unhealthy host")
		if _, err := host.client.GetStatus(); err == nil {
			// assume the host is healthy if we can get its status
			log.Info("host is now healthy")
			host.Healthy = true
			host.Checks = 0
			continue
		}

		host.Checks++
		if host.Checks >= s.maxHostChecks {
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

func (s *Scheduler) HandleVolumeEvent(e *VolumeEvent) {
	log := s.logger.New("fn", "HandleVolumeEvent", "event.type", e.Type, "vol.type", e.Volume.Type, "vol.id", e.Volume.ID)

	log.Info("handling volume event")
	switch e.Type {
	case VolumeEventTypeCreate:
		if _, ok := s.volumes[e.Volume.ID]; !ok {
			s.volumes[e.Volume.ID] = e.Volume
			s.persistVolume(e.Volume)
		}
	case VolumeEventTypeDestroy:
		if vol, ok := s.volumes[e.Volume.ID]; ok && vol.GetState() != ct.VolumeStateDestroyed {
			vol.SetState(ct.VolumeStateDestroyed)
			s.persistVolume(vol)
		}
	case VolumeEventTypeController:
		s.handleControllerVolume(&e.Volume.Volume)
	}
}

func (s *Scheduler) handleControllerVolume(volume *ct.Volume) {
	vol, ok := s.volumes[volume.ID]
	if !ok {
		vol = &Volume{Volume: *volume}
		s.volumes[volume.ID] = vol
	}
	// if this is the first time we are hearing about a
	// decommissioned volume, mark it as decommissioned
	// and try starting any jobs which were waiting on
	// this volume (they will now either use a different
	// volume or get a new one)
	if volume.DecommissionedAt != nil && vol.DecommissionedAt == nil {
		vol.DecommissionedAt = volume.DecommissionedAt
		if vol.JobID == nil {
			return
		}
		if job, ok := s.jobs[*vol.JobID]; ok && job.State == JobStateBlocked {
			job.State = JobStatePending
			go s.StartJob(job)
		}
	}
}

func (s *Scheduler) HandleJobEvent(e *host.Event) {
	log := s.logger.New("fn", "HandleJobEvent", "job.id", e.JobID, "event.type", e.Event)

	log.Info("handling job event")
	job := s.handleActiveJob(e.Job)
	switch e.Event {
	case host.JobEventStart:
		log.Debug("handled job start event", "job", job)
	case host.JobEventStop:
		log.Debug("handled job stop event", "job", job)
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

	// lookup the job using the UUID part of the job ID (see the
	// description of Job.ID)
	id, err := cluster.ExtractUUID(hostJob.ID)
	if err != nil {
		// job has invalid ID, ignore (very unexpected)
		return nil
	}
	job, ok := s.jobs[id]
	if !ok {
		// this is the first time we have seen the job so
		// add it to s.jobs
		job = &Job{
			ID:        id,
			Type:      jobType,
			AppID:     appID,
			ReleaseID: releaseID,
			HostID:    activeJob.HostID,
			JobID:     hostJob.ID,
			Args:      hostJob.Config.Args,
		}
		s.jobs.Add(job)
	}

	if len(hostJob.Config.Volumes) > 0 {
		// ensure job.Volumes is accurate
		job.Volumes = make([]*Volume, 0, len(hostJob.Config.Volumes))

		for _, v := range hostJob.Config.Volumes {
			vol, ok := s.volumes[v.VolumeID]
			if !ok {
				// ignore volumes we don't know about as we simply
				// won't take them into account when placing jobs
				// (this is unlikely as we sync volumes before jobs)
				s.logger.Warn("ignoring unknown volume", "job.id", job.ID, "vol.id", v.VolumeID)
				continue
			}

			// either assign or unassign the job from the volume based on
			// the job's status
			previousJobID := vol.JobID
			if activeJob.Status == host.StatusStarting || activeJob.Status == host.StatusRunning {
				vol.JobID = &job.ID
			} else if !vol.DeleteOnStop {
				// only unassign if DeleteOnStop isn't set
				// (we don't want to try assigning it to other
				// jobs if it's about to be destroyed)
				vol.JobID = nil
			}

			// persist the volume if the job ID changed
			if previousJobID != vol.JobID {
				s.persistVolume(vol)
			}

			job.Volumes = append(job.Volumes, vol)
		}
	}

	// If the host ID of the active job is different to the host ID of the
	// in-memory job, then it shouldn't be running so just stop it.
	//
	// This can happen if an initial request to start the job fails but the
	// host does in fact start the job (e.g. the AddJob HTTP request timed
	// out), and in the meantime the job was started successfully on a
	// different host.
	if job.HostID != activeJob.HostID {
		s.logger.Warn("stopping job with incorrect host ID", "job.id", job.ID, "expected", job.HostID, "actual", activeJob.HostID)
		if host, ok := s.hosts[activeJob.HostID]; ok {
			if err := host.client.StopJob(hostJob.ID); err != nil {
				s.logger.Error("error stopping job", "job.id", job.ID, "host.id", activeJob.HostID, "err", err)
			}
		}
		return job
	}

	job.StartedAt = activeJob.StartedAt
	job.metadata = hostJob.Metadata
	job.exitStatus = activeJob.ExitStatus
	job.hostError = activeJob.Error

	// if the host job is running but has a service, wait for either
	// service or router events before marking the job as running
	if activeJob.Status == host.StatusRunning && job.Service() != "" {
		// TODO: consider adding a hard timeout to mark the job
		//	 as running in case we never get any events
		return job
	}

	s.handleJobStatus(job, activeJob.Status)

	return job
}

func (s *Scheduler) markAsStopped(job *Job) {
	s.handleJobStatus(job, host.StatusDone)
}

func (s *Scheduler) handleJobStatus(job *Job, status host.JobStatus) {
	log := s.logger.New("fn", "handleJobStatus", "job.id", job.JobID, "app.id", job.AppID, "release.id", job.ReleaseID, "job.type", job.Type)

	// update the job's state, keeping a reference to the previous state
	previousState := job.State
	switch status {
	case host.StatusStarting:
		// only transition from pending -> starting (avoids
		// out of order events marking jobs as starting which
		// are actually running / stopped)
		if job.State == "" || job.State == JobStatePending {
			job.State = JobStateStarting
		}
	case host.StatusRunning:
		if job.State != JobStateStopping {
			job.State = JobStateRunning
		}
	case host.StatusDone, host.StatusCrashed, host.StatusFailed:
		delete(s.routerBackends, job.JobID)
		job.State = JobStateStopped
	}

	// if the job's state has changed, persist it to the controller
	if job.State != previousState {
		log.Info("handling job status change", "from", previousState, "to", job.State)
		s.persistJob(job)
	}

	// ensure jobs started as part of a formation change have a known formation
	if job.metadata["flynn-controller.formation"] == "true" && job.Formation == nil {
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
				jobs[job.ID] = job

				// only log an error if the state changed (so we don't
				// keep logging it in periodic SyncJobs calls)
				if job.State != previousState {
					log.Error("error looking up formation for job", "err", err)
				}
				return
			}
			formation = s.handleFormation(ef)
		}
		job.Formation = formation
	}

	// if the job was not started as part of a formation, then we are done
	if job.Formation == nil {
		return
	}

	// if we are not the leader, then we are done
	if !s.IsLeader() {
		return
	}

	// if the job has just transitioned to the stopped state, check if we
	// expect it to be running, and if we do, restart it
	if previousState != JobStateStopped && job.State == JobStateStopped {
		if diff := s.formationDiff(job.Formation); diff[job.Type] > 0 {
			s.restartJob(job)
		}
	}

	// trigger a rectify for the job's formation in case we have too many
	// jobs of the given type and we need to stop some
	s.triggerRectify(job.Formation.key())
}

func (s *Scheduler) persistJob(job *Job) {
	s.persistControllerJob(job.ControllerJob())
}

func (s *Scheduler) persistVolume(vol *Volume) {
	if s.isLeader == nil || *s.isLeader {
		s.controllerPersist <- vol.ControllerVolume()
	}
}

// persistControllerJob triggers the ControllerPersistLoop goroutine to persist
// the job to the controller, but only if the scheduler either doesn't know the
// current leader (e.g. if this is the first scheduler to start) or it itself
// is the current leader to avoid states jumping back and forward in the
// database
func (s *Scheduler) persistControllerJob(job *ct.Job) {
	if s.isLeader == nil || *s.isLeader {
		s.controllerPersist <- job
	}
}

func (s *Scheduler) handleFormation(ef *ct.ExpandedFormation) (formation *Formation) {
	log := s.logger.New("fn", "handleFormation", "app.id", ef.App.ID, "release.id", ef.Release.ID, "updated", ef.UpdatedAt)

	defer func() {
		// subscribe to any release services so we know when to mark
		// service jobs as running
		processes := Processes(ef.Processes)
		for _, proc := range ef.Release.Processes {
			if proc.Service == "" {
				continue
			}
			service, ok := s.services[proc.Service]
			if !ok {
				if processes.IsEmpty() {
					continue
				}
				service = NewService(proc.Service, s.serviceEvents, s.logger)
				s.services[proc.Service] = service
			}
			if processes.IsEmpty() {
				delete(service.Formations, formation.key())
				if len(service.Formations) == 0 {
					service.Close()
					delete(s.services, proc.Service)
				}
			} else {
				service.Formations[formation.key()] = struct{}{}
			}
		}

		if formation.PendingScaleRequest != nil {
			s.triggerRectify(formation.key())
		}

		// ensure the formation has the correct omni job counts
		if formation.RectifyOmni(s.activeHostCount()) {
			s.triggerRectify(formation.key())
		}

		// update any formation-less jobs
		if jobs, ok := s.formationlessJobs[formation.key()]; ok {
			for _, job := range jobs {
				job.Formation = formation
			}
			s.triggerRectify(formation.key())
			delete(s.formationlessJobs, formation.key())
		}
	}()

	formation = s.formations.Get(ef.App.ID, ef.Release.ID)
	if formation == nil {
		log.Info("adding new formation", "processes", ef.Processes)
		formation = s.formations.Add(NewFormation(ef))
	} else {
		// ignore stale formation changes
		if formation.UpdatedAt.After(ef.UpdatedAt) {
			log.Warn("ignoring stale formation change", "diff", formation.UpdatedAt.Sub(ef.UpdatedAt))
			return
		}
		formation.UpdatedAt = ef.UpdatedAt
		formation.PendingScaleRequest = ef.PendingScaleRequest

		diff := Processes(ef.Processes).Diff(formation.OriginalProcesses)
		if diff.IsEmpty() && utils.FormationTagsEqual(formation.Tags, ef.Tags) {
			return
		}

		// do not completely scale down critical apps for which this is the only active formation
		// (this prevents for example scaling down discoverd which breaks the cluster)
		if diff.IsScaleDownOf(formation.OriginalProcesses) && formation.App.Critical() && s.activeFormationCount(formation.App.ID) < 2 {
			log.Info("refusing to scale down critical app")
			return
		}

		log.Info("updating processes and tags of existing formation", "processes", ef.Processes, "tags", ef.Tags)
		formation.Tags = ef.Tags
		formation.SetProcesses(ef.Processes)
	}
	s.triggerRectify(formation.key())
	return
}

func (s *Scheduler) handleSink(sink *ct.Sink) {
	log := s.logger.New("fn", "handleSink", "sink.id", sink.ID)

	if sink.Config == nil {
		log.Info("removing deleted sink")
		s.removeSink(sink)
		return
	}

	existing, ok := s.sinks[sink.ID]
	if !ok {
		log.Info("adding new sink", "sink.kind", sink.Kind)
		s.addSink(sink)
		return
	}

	if !reflect.DeepEqual(existing.Config, sink.Config) {
		log.Info("updating config of existing sink")
		s.addSink(sink)
	}
}

func (s *Scheduler) removeSink(sink *ct.Sink) {
	for _, host := range s.hosts {
		if err := host.RemoveSink(sink.ID); err != nil {
			// just log the error, SyncSinks will try removing the sink again
			// if it still exists on the host
			s.logger.Error("error removing sink", "host.id", host.ID, "err", err)
		}
	}
	delete(s.sinks, sink.ID)
}

func (s *Scheduler) addSink(sink *ct.Sink) {
	s.sinks[sink.ID] = sink
	for _, host := range s.hosts {
		if err := host.AddSink(sink); err != nil {
			// just log the error, SyncSinks will try adding the sink again
			// if it doesn't exist on the host
			s.logger.Error("error adding sink", "host.id", host.ID, "sink.id", sink.ID, "err", err)
		}
	}
}

func (s *Scheduler) triggerRectify(key utils.FormationKey) {
	s.rectifyBatch[key] = struct{}{}
	select {
	case s.rectify <- struct{}{}:
	default:
	}
}

func (s *Scheduler) stopJobOfType(f *Formation, typ string) (err error) {
	log := s.logger.New("fn", "stopJobOfType", "app.id", f.App.ID, "release.id", f.Release.ID, "job.type", typ)
	log.Info(fmt.Sprintf("stopping %s job", typ))

	defer func() {
		if err != nil {
			log.Error(fmt.Sprintf("error stopping %s job", typ), "err", err)
		}
	}()

	job, err := s.findJobToStop(f, typ)
	if err != nil {
		return err
	}
	return s.stopJob(job)
}

func (s *Scheduler) stopJob(job *Job) error {
	log := s.logger.New("fn", "stopJob", "job.id", job.ID, "job.type", job.Type, "job.state", job.State)
	log.Info("stopping job")

	switch job.State {
	case JobStatePending:
		// If it's a pending job with a HostID, then it has been
		// placed in the cluster but we are yet to receive a
		// "starting" event, so we need to explicitly stop it.
		if job.HostID != "" {
			break
		}

		// If it's a pending job which hasn't been placed, we
		// are either in the process of starting it, or it is
		// scheduled to start in the future.
		//
		// Jobs being actively started can just be marked as
		// stopped, causing the StartJob goroutine to fail the
		// next time it tries to place the job.
		//
		// Scheduled jobs need the restart timer cancelling, but
		// also marked as stopped so that if the timer has already
		// fired, it won't actually be placed in the cluster.
		log.Info("stopping pending job", "job.id", job.ID)
		job.State = JobStateStopped
		s.persistJob(job)
		if job.restartTimer != nil {
			job.restartTimer.Stop()
		}
		return nil
	case JobStateStopped:
		// job already stopped, nothing to do
		return nil
	}

	host, ok := s.hosts[job.HostID]
	if !ok {
		return fmt.Errorf("unknown host: %q", job.HostID)
	}

	// set the state to JobStateStopping in case a StartJob goroutine is
	// still trying to start the job, in which case it will get an
	// ErrJobNotPending error on the next call to PlaceJob
	if job.State != JobStateStopping {
		job.State = JobStateStopping
		s.persistJob(job)
	}

	routerBackend := s.routerBackends[job.JobID]
	go func() {
		log := log.New("host.id", job.HostID)

		if routerBackend != nil {
			log.Info("signalling job to deregister from service discovery")
			if err := host.client.DiscoverdDeregisterJob(job.JobID); err == nil {
				log.Info("waiting for routers to stop sending requests")
				select {
				case <-routerBackend.Drained:
				case <-time.After(routerDrainTimeout):
					log.Warn("timed out waiting for routers to stop sending requests")
				}
			} else {
				log.Error("error signalling job to deregister from service discovery", "err", err)
			}
		}

		log.Info("requesting host to stop job")
		if err := host.client.StopJob(job.JobID); err != nil {
			// when an error happens, we don't know if the job actually
			// stopped or not, but just log the error instead of retrying
			// and let the next SyncJobs routine determine if another
			// attempt at stopping the job is necessary
			log.Error("error requesting host to stop job", "err", err)
		}
	}()
	return nil
}

// findJobToStop finds a job from the given formation and type which should be
// stopped, choosing pending jobs if present, and the most recently started job
// otherwise
func (s *Scheduler) findJobToStop(f *Formation, typ string) (*Job, error) {
	var found *Job
	for _, job := range s.jobs.WithFormationAndType(f, typ) {
		switch job.State {
		case JobStatePending:
			return job, nil
		case JobStateStarting, JobStateRunning:
			// if the job is on a host which is shutting down,
			// return it (it is about to stop anyway, and this
			// avoids a race where modifying the omni counts to
			// remove a shut down host could cause a subsequent
			// rectify to stop a job on an active host before the
			// shutting down host's job has stopped)
			if host, ok := s.hosts[job.HostID]; ok && host.Shutdown {
				return job, nil
			}

			// return the most recent job (which is the first in
			// the slice we are iterating over) if none of the
			// above cases match, preferring starting jobs to
			// running ones
			if found == nil || found.State == JobStateRunning && job.State == JobStateStarting {
				found = job
			}
		}
	}
	if found == nil {
		return nil, fmt.Errorf("no %s jobs running", typ)
	}
	return found, nil
}

func jobConfig(job *Job, hostID string) *host.Job {
	j := utils.JobConfig(job.Formation.ExpandedFormation, job.Type, hostID, job.ID)
	j.Config.Volumes = make([]host.VolumeBinding, len(job.Volumes))
	for i, vol := range job.Volumes {
		j.Config.Volumes[i] = host.VolumeBinding{
			Target:       vol.Path,
			VolumeID:     vol.ID,
			Writeable:    true,
			DeleteOnStop: vol.DeleteOnStop,
		}
	}
	return j
}

func (s *Scheduler) Pause() {
	s.logger.Info("pausing scheduler")
	s.pause <- struct{}{}
	s.logger.Info("scheduler paused")
}

func (s *Scheduler) Resume() {
	s.logger.Info("resuming scheduler")
	s.resume <- struct{}{}
	s.logger.Info("scheduler resumed")
}

func (s *Scheduler) Stop() error {
	log := s.logger.New("fn", "Stop")
	log.Info("stopping scheduler loop")
	s.stopOnce.Do(func() {
		close(s.stop)
	})
	return nil
}

func (s *Scheduler) RunningJobs() map[string]*Job {
	jobs := s.InternalState().Jobs
	runningJobs := make(map[string]*Job, len(jobs))
	for id, j := range jobs {
		if j.IsRunning() {
			runningJobs[id] = j
		}
	}
	return runningJobs
}

func (s *Scheduler) restartJob(job *Job) {
	restarts := job.Restarts
	// reset the restart count if it has been running for more than 5 minutes
	if !job.StartedAt.IsZero() && job.StartedAt.Before(time.Now().Add(-5*time.Minute)) {
		restarts = 0
	}
	backoff := s.getBackoffDuration(restarts)

	// create a new job so its state is tracked separately from the job
	// it is replacing
	newJob := &Job{
		ID:        s.generateJobUUID(),
		Type:      job.Type,
		AppID:     job.AppID,
		ReleaseID: job.ReleaseID,
		Formation: job.Formation,
		RunAt:     typeconv.TimePtr(time.Now().Add(backoff)),
		StartedAt: time.Now(),
		State:     JobStatePending,
		Restarts:  restarts + 1,
		Args:      job.Args,
	}
	s.jobs.Add(newJob)

	// persist the job so that it appears as pending in the database
	s.persistJob(newJob)

	s.logger.Info("scheduling job restart", "fn", "restartJob", "old_job.id", job.ID, "new_job.id", newJob.ID, "attempts", newJob.Restarts, "delay", backoff)
	newJob.restartTimer = time.AfterFunc(backoff, func() { s.StartJob(newJob) })
}

func (s *Scheduler) getBackoffDuration(restarts uint) time.Duration {
	switch {
	case restarts < 5:
		return 0
	case restarts < 15:
		return 10 * time.Second
	default:
		return 30 * time.Second
	}
}

func (s *Scheduler) startHTTPServer(port string) {
	log := s.logger.New("fn", "startHTTPServer")

	http.HandleFunc("/debug/state", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(s.InternalState())
	})

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
	s.logger.Info("starting sync jobs ticker", "duration", d)
	go func() {
		for range time.Tick(d) {
			s.triggerSyncJobs()
		}
	}()
}

func (s *Scheduler) tickSyncFormations(d time.Duration) {
	s.logger.Info("starting sync formations ticker", "duration", d)
	go func() {
		for range time.Tick(d) {
			s.triggerSyncFormations()
		}
	}()
}

func (s *Scheduler) tickSyncSinks(d time.Duration) {
	s.logger.Info("starting sync log sinks ticker", "duration", d)
	go func() {
		for range time.Tick(d) {
			s.triggerSyncSinks()
		}
	}()
}

func (s *Scheduler) tickSyncVolumes(d time.Duration) {
	s.logger.Info("starting sync volumes ticker", "duration", d)
	go func() {
		for range time.Tick(d) {
			s.triggerSyncVolumes()
		}
	}()
}

func (s *Scheduler) tickSyncHosts(d time.Duration) {
	s.logger.Info("starting sync hosts ticker", "duration", d)
	go func() {
		for range time.Tick(d) {
			s.triggerSyncHosts()
		}
	}()
}

func (s *Scheduler) rectifyAll() {
	for key := range s.formations {
		s.triggerRectify(key)
	}
}

func (s *Scheduler) triggerSyncJobs() {
	select {
	case s.syncJobs <- struct{}{}:
	default:
	}
}

func (s *Scheduler) triggerSyncFormations() {
	select {
	case s.syncFormations <- struct{}{}:
	default:
	}
}

func (s *Scheduler) triggerSyncSinks() {
	select {
	case s.syncSinks <- struct{}{}:
	default:
	}
}

func (s *Scheduler) triggerSyncVolumes() {
	select {
	case s.syncVolumes <- struct{}{}:
	default:
	}
}

func (s *Scheduler) triggerSyncHosts() {
	select {
	case s.syncHosts <- struct{}{}:
	default:
	}
}

func (s *Scheduler) triggerHostChecks() {
	select {
	case s.hostChecks <- struct{}{}:
	default:
	}
}
