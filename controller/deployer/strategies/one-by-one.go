package strategy

import ct "github.com/flynn/flynn/controller/types"

func oneByOne(d *Deploy) error {
	log := d.logger.New("fn", "oneByOne")
	log.Info("starting one-by-one deployment")

	oldProcesses := d.Processes
	newProcesses := make(map[string]int, len(oldProcesses))

	olog := log.New("release_id", d.OldReleaseID)
	nlog := log.New("release_id", d.NewReleaseID)
	for typ, num := range d.Processes {
		for i := 0; i < num; i++ {
			nlog.Info("scaling new formation up by one", "type", typ)
			newProcesses[typ]++
			if err := d.client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.NewReleaseID,
				Processes: newProcesses,
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
			oldProcesses[typ]--
			if err := d.client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.OldReleaseID,
				Processes: oldProcesses,
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
