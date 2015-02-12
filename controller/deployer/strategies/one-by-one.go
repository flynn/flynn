package strategy

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func oneByOne(l log15.Logger, client *controller.Client, d *ct.Deployment, events chan<- ct.DeploymentEvent) error {
	log := l.New("fn", "oneByOne", "deployment_id", d.ID, "app_id", d.AppID)
	log.Info("starting one-by-one deployment")

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

	oldProcesses := f.Processes
	newProcesses := make(map[string]int, len(oldProcesses))

	nlog := log.New("release_id", d.NewReleaseID)
	for typ, num := range f.Processes {
		for i := 0; i < num; i++ {
			nlog.Info("scaling new formation up by one", "type", typ)
			newProcesses[typ]++
			if err := client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.NewReleaseID,
				Processes: newProcesses,
			}); err != nil {
				nlog.Error("error scaling new formation up by one", "type", typ, "err", err)
				return err
			}
			events <- ct.DeploymentEvent{
				ReleaseID: d.NewReleaseID,
				JobState:  "starting",
				JobType:   typ,
			}

			nlog.Info("waiting for job up event", "type", typ)
			if err := waitForJobEvents(jobStream, events, d.NewReleaseID, jobEvents{typ: {"up": 1}}, nlog); err != nil {
				nlog.Error("error waiting for job up event", "err", err)
				return err
			}

			olog.Info("scaling old formation down by one", "type", typ)
			oldProcesses[typ]--
			if err := client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.OldReleaseID,
				Processes: oldProcesses,
			}); err != nil {
				olog.Error("error scaling old formation down by one", "type", typ, "err", err)
				return err
			}
			events <- ct.DeploymentEvent{
				ReleaseID: d.OldReleaseID,
				JobState:  "stopping",
				JobType:   typ,
			}
			olog.Info("waiting for job down event", "type", typ)
			if err := waitForJobEvents(jobStream, events, d.OldReleaseID, jobEvents{typ: {"down": 1}}, olog); err != nil {
				olog.Error("error waiting for job down event", "err", err)
				return err
			}
		}
	}
	log.Info("finished one-by-one deployment")
	return nil
}
