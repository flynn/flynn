package strategy

import ct "github.com/flynn/flynn/controller/types"

func oneByOne(d *Deploy) error {
	log := d.logger.New("fn", "oneByOne")
	log.Info("starting one-by-one deployment")

	olog := log.New("release_id", d.OldReleaseID)
	nlog := log.New("release_id", d.NewReleaseID)
	for typ, num := range d.Processes {
		for i := d.newReleaseState[typ]; i < num; i++ {
			nlog.Info("scaling new formation up by one", "type", typ)
			d.newReleaseState[typ]++
			if err := d.client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.NewReleaseID,
				Processes: d.newReleaseState,
			}); err != nil {
				nlog.Error("error scaling new formation up by one", "type", typ, "err", err)
				return err
			}
			d.deployEvents <- ct.DeploymentEvent{
				ReleaseID: d.NewReleaseID,
				JobState:  "starting",
				JobType:   typ,
			}

			nlog.Info("waiting for job up event", "type", typ)
			if err := d.waitForJobEvents(d.NewReleaseID, jobEvents{typ: {"up": 1}}, nlog); err != nil {
				nlog.Error("error waiting for job up event", "err", err)
				return err
			}

			olog.Info("scaling old formation down by one", "type", typ)
			d.oldReleaseState[typ]--
			if err := d.client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.OldReleaseID,
				Processes: d.oldReleaseState,
			}); err != nil {
				olog.Error("error scaling old formation down by one", "type", typ, "err", err)
				return err
			}
			d.deployEvents <- ct.DeploymentEvent{
				ReleaseID: d.OldReleaseID,
				JobState:  "stopping",
				JobType:   typ,
			}
			olog.Info("waiting for job down event", "type", typ)
			if err := d.waitForJobEvents(d.OldReleaseID, jobEvents{typ: {"down": 1}}, olog); err != nil {
				olog.Error("error waiting for job down event", "err", err)
				return err
			}
		}
	}
	log.Info("finished one-by-one deployment")
	return nil
}
