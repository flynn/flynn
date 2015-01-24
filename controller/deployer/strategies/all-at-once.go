package strategy

import (
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func allAtOnce(client *controller.Client, d *ct.Deployment, events chan<- ct.DeploymentEvent) error {
	jobStream := make(chan *ct.JobEvent)
	stream, err := client.StreamJobEvents(d.AppID, 0, jobStream)
	if err != nil {
		return err
	}
	defer stream.Close()

	f, err := client.GetFormation(d.AppID, d.OldReleaseID)
	if err != nil {
		return err
	}

	if err := client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.NewReleaseID,
		Processes: f.Processes,
	}); err != nil {
		return err
	}
	expect := make(jobEvents)
	for typ, n := range f.Processes {
		for i := 0; i < n; i++ {
			events <- ct.DeploymentEvent{
				ReleaseID: d.NewReleaseID,
				JobState:  "starting",
				JobType:   typ,
			}
		}
		expect[d.NewReleaseID] = map[string]map[string]int{typ: {"up": n}}
	}
	if err := waitForJobEvents(jobStream, events, expect); err != nil {
		return err
	}
	// scale to 0
	if err := client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.OldReleaseID,
	}); err != nil {
		return err
	}
	expect = make(jobEvents)
	for typ, n := range f.Processes {
		for i := 0; i < n; i++ {
			events <- ct.DeploymentEvent{
				ReleaseID: d.OldReleaseID,
				JobState:  "stopping",
				JobType:   typ,
			}
		}
		expect[d.OldReleaseID] = map[string]map[string]int{typ: {"down": n}}
	}
	if err := waitForJobEvents(jobStream, events, expect); err != nil {
		return err
	}
	return nil
}
