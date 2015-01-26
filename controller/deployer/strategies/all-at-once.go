package strategy

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func allAtOnce(l log15.Logger, client *controller.Client, d *ct.Deployment, events chan<- ct.DeploymentEvent) error {
	log := l.New("fn", "allAtOnce")
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
		log.Error("Failed to fetch the old formation", "at", "get_formation", "err", err)
		return err
	}

	if err := client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.NewReleaseID,
		Processes: f.Processes,
	}); err != nil {
		log.Error("Failed to start processes", "at", "start_processes", "err", err)
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
		log.Error("Error during waiting for job events", "at", "wait", "err", err)
		return err
	}
	// scale to 0
	if err := client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.OldReleaseID,
	}); err != nil {
		log.Error("Failed to stop processes", "at", "stop_processes", "err", err)
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
		log.Error("Error during waiting for job events", "at", "wait", "err", err)
		return err
	}
	log.Info("Done")
	return nil
}
