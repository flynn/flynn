package strategy

import (
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func oneByOne(client *controller.Client, d *ct.Deployment, events chan<- ct.DeploymentEvent) error {
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

	oldFormation := f.Processes
	newFormation := map[string]int{}

	for typ, num := range f.Processes {
		for i := 0; i < num; i++ {
			// start one process
			newFormation[typ]++
			if err := client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.NewReleaseID,
				Processes: newFormation,
			}); err != nil {
				return err
			}
			events <- ct.DeploymentEvent{
				ReleaseID: d.NewReleaseID,
				JobState:  "starting",
				JobType:   typ,
			}
			if err := waitForJobEvents(jobStream, events, jobEvents{d.NewReleaseID: {typ: {"up": 1}}}); err != nil {
				return err
			}
			// stop one process
			oldFormation[typ]--
			if err := client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.OldReleaseID,
				Processes: oldFormation,
			}); err != nil {
				return err
			}
			events <- ct.DeploymentEvent{
				ReleaseID: d.OldReleaseID,
				JobState:  "stopping",
				JobType:   typ,
			}
			if err := waitForJobEvents(jobStream, events, jobEvents{d.OldReleaseID: {typ: {"down": 1}}}); err != nil {
				return err
			}
		}
	}
	return nil
}
