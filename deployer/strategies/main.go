package strategy

import (
	"fmt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/deployer/types"
	"time"
)

type Performer interface {
	Perform(*deployer.Deployment, chan<- deployer.DeploymentEvent) error
}

func GetStrategy(strategy string, client *controller.Client) (Performer, error) {
	switch strategy {
	case "one-by-one":
		return &OneByOne{client}, nil
	case "all-at-once":
		return &AllAtOnce{client}, nil
	default:
		return nil, fmt.Errorf("Unknown strategy '%s'!", strategy)
	}
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

func waitForJobEvents(events chan *ct.JobEvent, expected jobEvents) (lastID int64, jobID string, err error) {
	fmt.Printf("waiting for job events: %v", expected)
	actual := make(jobEvents)
	for {
	inner:
		select {
		case event := <-events:
			fmt.Printf("got job event: %s %s %s", event.Type, event.JobID, event.State)
			lastID = event.ID
			jobID = event.JobID
			if _, ok := actual[event.Type]; !ok {
				actual[event.Type] = make(map[string]int)
			}
			switch event.State {
			case "up":
				actual[event.Type]["up"] += 1
			case "down", "crashed":
				actual[event.Type]["down"] += 1
			default:
				break inner
			}
			if jobEventsEqual(expected, actual) {
				return
			}
		case <-time.After(60 * time.Second):
			return 0, "", fmt.Errorf("timed out waiting for job events: ", expected)
		}
	}
}
