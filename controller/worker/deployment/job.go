package deployment

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/worker/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/cluster"
	"gopkg.in/inconshreveable/log15.v2"
)

type jobIDState struct {
	jobID string
	state ct.JobState
}

type JobEventType int

const (
	JobEventTypeDiscoverd JobEventType = iota
	JobEventTypeController
	JobEventTypeError
)

// JobEvent is a wrapper around either a discoverd service event, a controller
// job event or a stream error, and helps the deploy to have a separate
// channel per release when waiting for job events
type JobEvent struct {
	Type           JobEventType
	DiscoverdEvent *discoverd.Event
	JobEvent       *ct.Job
	Error          error
}

type DeployJob struct {
	*ct.Deployment
	client       controller.Client
	deployEvents chan<- ct.DeploymentEvent

	// jobEvents is a map of release IDs to channels which receive job
	// events for that particular release, and is used rather than just
	// one channel for all events so that events received whilst waiting
	// for one release to scale aren't dropped if they are for another
	// release which may be scaled in the future
	jobEvents    map[string]chan *JobEvent
	jobEventsMtx sync.Mutex

	serviceNames    map[string]string
	serviceMeta     *discoverd.ServiceMeta
	useJobEvents    map[string]struct{}
	logger          log15.Logger
	oldRelease      *ct.Release
	newRelease      *ct.Release
	oldReleaseState map[string]int
	newReleaseState map[string]int
	knownJobStates  map[jobIDState]struct{}
	omni            map[string]struct{}
	hostCount       int
	stop            chan struct{}
}

// ReleaseJobEvents lazily creates and returns a channel of job events for the
// given release
func (d *DeployJob) ReleaseJobEvents(releaseID string) chan *JobEvent {
	d.jobEventsMtx.Lock()
	defer d.jobEventsMtx.Unlock()
	if ch, ok := d.jobEvents[releaseID]; ok {
		return ch
	}
	// give the channel a buffer so events for a release not being waited
	// on do not block events for releases being waited on
	ch := make(chan *JobEvent, 100)
	d.jobEvents[releaseID] = ch
	return ch
}

// JobEventErr sends the given error on job event channels for all releases.
//
// It does not close the channels because there are multiple publishers which
// could break (i.e. controller job events and discoverd service events), but
// everything will ultimately be closed when this error is received and the
// deferred stream closes kick in.
func (d *DeployJob) JobEventErr(err error) {
	d.jobEventsMtx.Lock()
	defer d.jobEventsMtx.Unlock()
	for _, ch := range d.jobEvents {
		ch <- &JobEvent{
			Type:  JobEventTypeError,
			Error: err,
		}
	}
}

func (d *DeployJob) isOmni(typ string) bool {
	_, ok := d.omni[typ]
	return ok
}

func (d *DeployJob) Perform() error {
	log := d.logger.New("fn", "Perform", "deployment_id", d.ID, "app_id", d.AppID)

	log.Info("validating deployment strategy")
	var deployFunc func() error
	switch d.Strategy {
	case "one-by-one":
		deployFunc = d.deployOneByOne
	case "all-at-once":
		deployFunc = d.deployAllAtOnce
	case "sirenia":
		deployFunc = d.deploySirenia
	case "discoverd-meta":
		deployFunc = d.deployDiscoverdMeta
	default:
		err := UnknownStrategyError{d.Strategy}
		log.Error("error validating deployment strategy", "err", err)
		return err
	}

	log.Info("determining cluster size")
	hosts, err := cluster.NewClient().Hosts()
	if err != nil {
		log.Error("error listing cluster hosts", "err", err)
		return err
	}
	d.hostCount = len(hosts)

	log.Info("determining current release state")
	oldRelease, err := d.client.GetRelease(d.OldReleaseID)
	if err != nil {
		log.Error("error getting new release", "release_id", d.NewReleaseID, "err", err)
		return err
	}
	d.oldRelease = oldRelease

	log.Info("determining release services and deployment state")
	release, err := d.client.GetRelease(d.NewReleaseID)
	if err != nil {
		log.Error("error getting new release", "release_id", d.NewReleaseID, "err", err)
		return err
	}
	d.newRelease = release
	for typ, proc := range release.Processes {
		if proc.Omni {
			d.omni[typ] = struct{}{}
		}
		if proc.Service == "" {
			log.Info(fmt.Sprintf("using job events for %s process type, no service defined", typ))
			d.useJobEvents[typ] = struct{}{}
			continue
		}

		d.serviceNames[typ] = proc.Service

		log.Info(fmt.Sprintf("using service discovery for %s process type", typ), "service", proc.Service)
		events := make(chan *discoverd.Event)
		stream, err := discoverd.NewService(proc.Service).Watch(events)
		if err != nil {
			log.Error("error creating service discovery watcher", "service", proc.Service, "err", err)
			return err
		}
		defer stream.Close()

	outer:
		for {
			select {
			case <-d.stop:
				return worker.ErrStopped
			case event, ok := <-events:
				if !ok {
					log.Error("error creating service discovery watcher, channel closed", "service", proc.Service)
					return fmt.Errorf("deployer: could not create watcher for service: %s", proc.Service)
				}
				switch event.Kind {
				case discoverd.EventKindCurrent:
					break outer
				case discoverd.EventKindServiceMeta:
					d.serviceMeta = event.ServiceMeta
				case discoverd.EventKindUp:
					releaseID, ok := event.Instance.Meta["FLYNN_RELEASE_ID"]
					if !ok {
						continue
					}
					switch releaseID {
					case d.OldReleaseID:
						d.oldReleaseState[typ]++
					case d.NewReleaseID:
						d.newReleaseState[typ]++
					}
				}
			case <-time.After(5 * time.Second):
				log.Error("error creating service discovery watcher, timeout reached", "service", proc.Service)
				return fmt.Errorf("deployer: could not create watcher for service: %s", proc.Service)
			}
		}
		go func() {
			for {
				event, ok := <-events
				if !ok {
					// this usually means deferred cleanup is in progress, but send an error
					// in case the deploy is still waiting for an event which will now not come.
					d.JobEventErr(errors.New("unexpected close of service event stream"))
					return
				}
				if event.Instance == nil {
					continue
				}
				if id, ok := event.Instance.Meta["FLYNN_APP_ID"]; !ok || id != d.AppID {
					continue
				}
				releaseID, ok := event.Instance.Meta["FLYNN_RELEASE_ID"]
				if !ok {
					continue
				}
				d.ReleaseJobEvents(releaseID) <- &JobEvent{
					Type:           JobEventTypeDiscoverd,
					DiscoverdEvent: event,
				}
			}
		}()
	}

	log.Info("getting job event stream")
	jobEvents := make(chan *ct.Job)
	stream, err := d.client.StreamJobEvents(d.AppID, jobEvents)
	if err != nil {
		log.Error("error getting job event stream", "err", err)
		return err
	}
	defer stream.Close()
	go func() {
		for {
			event, ok := <-jobEvents
			if !ok {
				d.JobEventErr(errors.New("unexpected close of job event stream"))
				return
			}
			d.ReleaseJobEvents(event.ReleaseID) <- &JobEvent{
				Type:     JobEventTypeController,
				JobEvent: event,
			}
		}
	}()

	log.Info("getting current jobs")
	jobs, err := d.client.JobList(d.AppID)
	if err != nil {
		log.Error("error getting current jobs", "err", err)
		return err
	}
	for _, job := range jobs {
		if job.State != ct.JobStateUp {
			continue
		}
		if _, ok := d.useJobEvents[job.Type]; !ok {
			continue
		}

		// track the jobs so we can drop any events received between
		// connecting the job stream and getting the list of jobs
		d.knownJobStates[jobIDState{job.ID, ct.JobStateUp}] = struct{}{}

		switch job.ReleaseID {
		case d.OldReleaseID:
			d.oldReleaseState[job.Type]++
		case d.NewReleaseID:
			d.newReleaseState[job.Type]++
		}
	}

	log.Info(
		"determined deployment state",
		"original", d.Processes,
		"old_release", d.oldReleaseState,
		"new_release", d.newReleaseState,
	)
	return deployFunc()
}

func (d *DeployJob) waitForJobEvents(releaseID string, expected ct.JobEvents, log log15.Logger) error {
	actual := make(ct.JobEvents)

	handleEvent := func(jobID, typ string, state ct.JobState) {
		// ignore pending events
		if state == ct.JobStatePending {
			return
		}

		// don't send duplicate events
		if _, ok := d.knownJobStates[jobIDState{jobID, state}]; ok {
			return
		}
		d.knownJobStates[jobIDState{jobID, state}] = struct{}{}

		if _, ok := actual[typ]; !ok {
			actual[typ] = make(map[ct.JobState]int)
		}
		actual[typ][state] += 1
		d.deployEvents <- ct.DeploymentEvent{
			ReleaseID: releaseID,
			JobState:  state,
			JobType:   typ,
		}
	}

	jobEvents := d.ReleaseJobEvents(releaseID)
	for {
		select {
		case <-d.stop:
			return worker.ErrStopped
		case e := <-jobEvents:
			switch e.Type {
			case JobEventTypeDiscoverd:
				event := e.DiscoverdEvent
				if !event.Kind.Any(discoverd.EventKindUp, discoverd.EventKindUpdate) {
					continue
				}
				typ, ok := event.Instance.Meta["FLYNN_PROCESS_TYPE"]
				if !ok {
					continue
				}
				if _, ok := d.useJobEvents[typ]; ok {
					continue
				}
				jobID, ok := event.Instance.Meta["FLYNN_JOB_ID"]
				if !ok {
					continue
				}
				log.Info("got service event", "job.id", jobID, "job.type", typ, "job.state", event.Kind)
				handleEvent(jobID, typ, ct.JobStateUp)
				if expected.Equals(actual) {
					return nil
				}
			case JobEventTypeController:
				event := e.JobEvent
				// if service discovery is being used for the job's type, ignore up events and fail
				// the deployment if we get a down event when waiting for the job to come up.
				if _, ok := d.useJobEvents[event.Type]; !ok {
					if event.State == ct.JobStateUp {
						continue
					}
					if expected[event.Type][ct.JobStateUp] > 0 && event.IsDown() {
						handleEvent(event.ID, event.Type, ct.JobStateDown)
						return fmt.Errorf("%s process type failed to start, got %s job event", event.Type, event.State)
					}
				}

				log.Info("got job event", "job.id", event.ID, "job.type", event.Type, "job.state", event.State)
				if event.State == ct.JobStateStarting {
					continue
				}
				if _, ok := actual[event.Type]; !ok {
					actual[event.Type] = make(map[ct.JobState]int)
				}
				handleEvent(event.ID, event.Type, event.State)
				if event.HostError != nil {
					return fmt.Errorf("deployer: %s job failed to start: %s", event.Type, *event.HostError)
				}
				if expected.Equals(actual) {
					return nil
				}
			case JobEventTypeError:
				return e.Error
			}
		case <-time.After(time.Duration(d.DeployTimeout) * time.Second):
			return fmt.Errorf("timed out waiting for job events: %v", expected)
		}
	}
}
