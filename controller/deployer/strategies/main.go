package strategy

import (
	"fmt"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/stream"
)

type UnknownStrategyError struct {
	Name string
}

func (e UnknownStrategyError) Error() string {
	return fmt.Sprintf(`deployer: unknown strategy "%s"`, e.Name)
}

type PerformFunc func(d *Deploy) error

var performFuncs = map[string]PerformFunc{
	"all-at-once": allAtOnce,
	"one-by-one":  oneByOne,
}

func Perform(d *ct.Deployment, client *controller.Client, deployEvents chan<- ct.DeploymentEvent, logger log15.Logger) error {
	log := logger.New("fn", "Perform", "deplyment_id", d.ID, "app_id", d.AppID)

	log.Info("validating deployment strategy")
	performFunc, ok := performFuncs[d.Strategy]
	if !ok {
		err := UnknownStrategyError{d.Strategy}
		log.Error("error validating deployment strategy", "err", err)
		return err
	}

	deploy := &Deploy{
		Deployment:      d,
		client:          client,
		deployEvents:    deployEvents,
		logger:          logger,
		newReleaseState: make(map[string]int, len(d.Processes)),
		oldReleaseState: make(map[string]int, len(d.Processes)),
		knownJobs:       make(map[string]struct{}),
	}

	log.Info("connecting job event stream")
	if err := deploy.connectEventStream(); err != nil {
		log.Error("error connecting job event stream", "err", err)
		return err
	}
	defer deploy.closeEventStream()

	log.Info("determining deployment state")
	c, err := cluster.NewClient()
	if err != nil {
		log.Error("error determining deployment state", "err", err)
		return err
	}
	hosts, err := c.ListHosts()
	if err != nil {
		log.Error("error determining deployment state", "err", err)
		return err
	}
	for _, host := range hosts {
		for _, job := range host.Jobs {
			appID := job.Metadata["flynn-controller.app"]
			releaseID := job.Metadata["flynn-controller.release"]
			if appID != d.AppID || releaseID != d.OldReleaseID && releaseID != d.NewReleaseID {
				continue
			}

			// track known jobs so we can drop any events received between
			// connecting the job stream and getting the list of jobs
			deploy.knownJobs[job.ID] = struct{}{}

			typ := job.Metadata["flynn-controller.type"]
			switch releaseID {
			case d.OldReleaseID:
				deploy.oldReleaseState[typ]++
			case d.NewReleaseID:
				deploy.newReleaseState[typ]++
			}
		}
	}
	log.Info(
		"determined deployment state",
		"original", deploy.Processes,
		"old_release", deploy.oldReleaseState,
		"new_release", deploy.newReleaseState,
	)
	return performFunc(deploy)
}

type Deploy struct {
	*ct.Deployment
	client          *controller.Client
	deployEvents    chan<- ct.DeploymentEvent
	logger          log15.Logger
	events          chan *ct.JobEvent
	stream          stream.Stream
	lastID          int64
	newReleaseState map[string]int
	oldReleaseState map[string]int
	knownJobs       map[string]struct{}
}

func (d *Deploy) closeEventStream() error {
	if d.stream != nil {
		return d.stream.Close()
	}
	return nil
}

var connectAttempts = attempt.Strategy{
	Total: 10 * time.Second,
	Delay: 500 * time.Millisecond,
}

func (d *Deploy) connectEventStream() error {
	events := make(chan *ct.JobEvent)
	var stream stream.Stream
	if err := connectAttempts.Run(func() (err error) {
		stream, err = d.client.StreamJobEvents(d.AppID, d.lastID, events)
		return
	}); err != nil {
		return err
	}
	d.events = events
	d.stream = stream
	return nil
}

// TODO: share with tests
func jobEventsEqual(expected, actual jobEvents) bool {
	for typ, events := range expected {
		diff, ok := actual[typ]
		if !ok {
			return false
		}
		for state, count := range events {
			if diff[state] != count {
				return false
			}
		}
	}
	return true
}

type jobEvents map[string]map[string]int

func (j jobEvents) count() int {
	var n int
	for _, procs := range j {
		for _, i := range procs {
			n += i
		}
	}
	return n
}

func (d *Deploy) waitForJobEvents(releaseID string, expected jobEvents, log log15.Logger) error {
	actual := make(jobEvents)

	for {
		select {
		case event, ok := <-d.events:
			if !ok {
				// the stream could close when deploying the controller, so try to reconnect
				log.Warn("reconnecting job event stream", "lastID", d.lastID)
				if err := d.connectEventStream(); err != nil {
					log.Error("error reconnecting job event stream", "err", err)
					return err
				}
				continue
			}
			if event.Job.ReleaseID != releaseID {
				continue
			}
			d.lastID = event.ID
			if _, ok := d.knownJobs[event.Job.ID]; ok {
				continue
			}
			log.Info("got job event", "type", event.Type, "state", event.State)
			if _, ok := actual[event.Type]; !ok {
				actual[event.Type] = make(map[string]int)
			}
			switch event.State {
			case "up":
				actual[event.Type]["up"] += 1
				d.deployEvents <- ct.DeploymentEvent{
					ReleaseID: releaseID,
					JobState:  "up",
					JobType:   event.Type,
				}
			case "down":
				actual[event.Type]["down"] += 1
				d.deployEvents <- ct.DeploymentEvent{
					ReleaseID: releaseID,
					JobState:  "down",
					JobType:   event.Type,
				}
			case "crashed":
				actual[event.Type]["crashed"] += 1
				d.deployEvents <- ct.DeploymentEvent{
					ReleaseID: releaseID,
					JobState:  "crashed",
					JobType:   event.Type,
				}
				return fmt.Errorf("job crashed!")
			}
			if jobEventsEqual(expected, actual) {
				return nil
			}
		case <-time.After(60 * time.Second):
			return fmt.Errorf("timed out waiting for job events: ", expected)
		}
	}
	return nil
}
