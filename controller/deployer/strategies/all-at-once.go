package strategy

import ct "github.com/flynn/flynn/controller/types"

func allAtOnce(d *Deploy) error {
	log := d.logger.New("fn", "allAtOnce")
	log.Info("starting all-at-once deployment")

	expected := make(jobEvents)
	for typ, n := range d.Processes {
		total := n
		if d.isOmni(typ) {
			total *= d.hostCount
		}
		existing := d.newReleaseState[typ]
		for i := existing; i < total; i++ {
			d.deployEvents <- ct.DeploymentEvent{
				ReleaseID: d.NewReleaseID,
				JobState:  "starting",
				JobType:   typ,
			}
		}
		expected[typ] = map[string]int{"up": total - existing}
	}
	if expected.Count() > 0 {
		log := log.New("release_id", d.NewReleaseID)
		log.Info("creating new formation", "processes", d.Processes)
		if err := d.client.PutFormation(&ct.Formation{
			AppID:     d.AppID,
			ReleaseID: d.NewReleaseID,
			Processes: d.Processes,
		}); err != nil {
			log.Error("error creating new formation", "err", err)
			return err
		}

		log.Info("waiting for job events", "expected", expected)
		if err := d.waitForJobEvents(d.NewReleaseID, expected, log); err != nil {
			log.Error("error waiting for job events", "err", err)
			return err
		}
	}

	expected = make(jobEvents)
	for typ := range d.Processes {
		existing := d.oldReleaseState[typ]
		for i := 0; i < existing; i++ {
			d.deployEvents <- ct.DeploymentEvent{
				ReleaseID: d.OldReleaseID,
				JobState:  "stopping",
				JobType:   typ,
			}
		}
		expected[typ] = map[string]int{"down": existing}
	}
	if expected.Count() > 0 {
		log := log.New("release_id", d.OldReleaseID)
		log.Info("scaling old formation to zero")
		if err := d.client.PutFormation(&ct.Formation{
			AppID:     d.AppID,
			ReleaseID: d.OldReleaseID,
		}); err != nil {
			log.Error("error scaling old formation to zero", "err", err)
			return err
		}

		log.Info("waiting for job events", "expected", expected)
		if err := d.waitForJobEvents(d.OldReleaseID, expected, log); err != nil {
			log.Error("error waiting for job events", "err", err)
			return err
		}
	}

	log.Info("finished all-at-once deployment")
	return nil
}
