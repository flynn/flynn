package strategy

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func allAtOnce(l log15.Logger, client *controller.Client, d *ct.Deployment, events chan<- ct.DeploymentEvent) error {
	log := l.New("fn", "allAtOnce", "deployment_id", d.ID, "app_id", d.AppID)
	log.Info("starting all-at-once deployment")

	log.Info("getting job event stream")
	jobStream := make(chan *ct.JobEvent)
	stream, err := client.StreamJobEvents(d.AppID, 0, jobStream)
	if err != nil {
		log.Error("error getting job event stream", "err", err)
		return err
	}
	defer stream.Close()

	olog := log.New("release_id", d.OldReleaseID)
	olog.Info("getting old formation")
	f, err := client.GetFormation(d.AppID, d.OldReleaseID)
	if err != nil {
		olog.Error("error getting old formation", "err", err)
		return err
	}

	nlog := log.New("release_id", d.NewReleaseID)
	nlog.Info("creating new formation", "processes", f.Processes)
	if err := client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.NewReleaseID,
		Processes: f.Processes,
	}); err != nil {
		nlog.Error("error creating new formation", "err", err)
		return err
	}

	expected := make(jobEvents)
	for typ, n := range f.Processes {
		for i := 0; i < n; i++ {
			events <- ct.DeploymentEvent{
				ReleaseID: d.NewReleaseID,
				JobState:  "starting",
				JobType:   typ,
			}
		}
		expected[typ] = map[string]int{"up": n}
	}
	nlog.Info("waiting for job events", "expected", expected)
	if err := waitForJobEvents(jobStream, events, d.NewReleaseID, expected, nlog); err != nil {
		nlog.Error("error waiting for job events", "err", err)
		return err
	}

	olog.Info("scaling old formation to zero")
	if err := client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.OldReleaseID,
	}); err != nil {
		log.Error("error scaling old formation to zero", "err", err)
		return err
	}

	expected = make(jobEvents)
	for typ, n := range f.Processes {
		for i := 0; i < n; i++ {
			events <- ct.DeploymentEvent{
				ReleaseID: d.OldReleaseID,
				JobState:  "stopping",
				JobType:   typ,
			}
		}
		expected[typ] = map[string]int{"down": n}
	}
	olog.Info("waiting for job events", "expected", expected)
	if err := waitForJobEvents(jobStream, events, d.OldReleaseID, expected, olog); err != nil {
		olog.Error("error waiting for job events", "err", err)
		return err
	}
	log.Info("finished all-at-once deployment")
	return nil
}
