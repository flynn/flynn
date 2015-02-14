package strategy

import (
	"fmt"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

type PerformFunc func(log15.Logger, *controller.Client, *ct.Deployment, chan<- ct.DeploymentEvent) error

var performFuncs = map[string]PerformFunc{
	"all-at-once": allAtOnce,
	"one-by-one":  oneByOne,
}

func Get(strategy string) (PerformFunc, error) {
	if f, ok := performFuncs[strategy]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("Unknown strategy '%s'!", strategy)
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

func waitForJobEvents(events chan *ct.JobEvent, deployEvents chan<- ct.DeploymentEvent, releaseID string, expected jobEvents, log log15.Logger) error {
	actual := make(jobEvents)
outer:
	for {
	inner:
		select {
		case event, ok := <-events:
			if !ok {
				// if this happens, it means defer cleanup is in progress

				// TODO: this could also happen if the stream connection
				// dropped. handle that case
				break outer
			}
			if event.Job.ReleaseID != releaseID {
				continue
			}
			log.Info("got job event", "job_id", event.JobID, "type", event.Type, "state", event.State)
			if _, ok := actual[event.Type]; !ok {
				actual[event.Type] = make(map[string]int)
			}
			switch event.State {
			case "up":
				actual[event.Type]["up"] += 1
				deployEvents <- ct.DeploymentEvent{
					ReleaseID: releaseID,
					JobState:  "up",
					JobType:   event.Type,
				}
			case "down", "crashed":
				actual[event.Type]["down"] += 1
				deployEvents <- ct.DeploymentEvent{
					ReleaseID: releaseID,
					JobState:  "down",
					JobType:   event.Type,
				}
			case "failed":
				deployEvents <- ct.DeploymentEvent{
					ReleaseID: releaseID,
					JobState:  "failed",
					JobType:   event.Type,
				}
				return fmt.Errorf("deployer: %s job failed to start", event.Type)
			default:
				break inner
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
