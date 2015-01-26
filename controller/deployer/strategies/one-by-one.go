package strategy

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func oneByOne(l log15.Logger, client *controller.Client, d *ct.Deployment, events chan<- ct.DeploymentEvent) error {
	log := l.New("fn", "oneByOne")
	log.Info("Starting")

	jobStream := make(chan *ct.JobEvent)
	stream, err := client.StreamJobEvents(d.AppID, 0, jobStream)
	if err != nil {
		log.Error("Failed to create a job event stream", "at", "stream_job_events", "err", err)
		return err
	}
	defer stream.Close()

	f, err := client.GetFormation(d.AppID, d.OldReleaseID)
	if err != nil {
		log.Error("Failed fetching the old formation", "at", "get_formation", "err", err)
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
				log.Error("Failed starting a process", "at", "start_process", "err", err)
				return err
			}
			events <- ct.DeploymentEvent{
				ReleaseID: d.NewReleaseID,
				JobState:  "starting",
				JobType:   typ,
			}
			if err := waitForJobEvents(jobStream, events, jobEvents{d.NewReleaseID: {typ: {"up": 1}}}); err != nil {
				log.Error("Error during waiting for job events", "at", "wait", "err", err)
				return err
			}
			// stop one process
			oldFormation[typ]--
			if err := client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.OldReleaseID,
				Processes: oldFormation,
			}); err != nil {
				log.Error("Failed stopping a process", "at", "stop_process", "err", err)
				return err
			}
			events <- ct.DeploymentEvent{
				ReleaseID: d.OldReleaseID,
				JobState:  "stopping",
				JobType:   typ,
			}
			if err := waitForJobEvents(jobStream, events, jobEvents{d.OldReleaseID: {typ: {"down": 1}}}); err != nil {
				log.Error("Error during waiting for job events", "at", "wait", "err", err)
				return err
			}
		}
	}
	log.Info("Done")
	return nil
}
