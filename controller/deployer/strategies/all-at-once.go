package strategy

import ct "github.com/flynn/flynn/controller/types"

func allAtOnce(d *Deploy) error {
	log := d.logger.New("fn", "allAtOnce")
	log.Info("starting all-at-once deployment")

	olog := log.New("release_id", d.OldReleaseID)
	olog.Info("getting old formation")
	f, err := d.client.GetFormation(d.AppID, d.OldReleaseID)
	if err != nil {
		olog.Error("error getting old formation", "err", err)
		return err
	}

	nlog := log.New("release_id", d.NewReleaseID)
	nlog.Info("creating new formation", "processes", f.Processes)
	if err := d.client.PutFormation(&ct.Formation{
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
			d.deployEvents <- ct.DeploymentEvent{
				ReleaseID: d.NewReleaseID,
				JobState:  "starting",
				JobType:   typ,
			}
		}
		expected[typ] = map[string]int{"up": n}
	}
	nlog.Info("waiting for job events", "expected", expected)
	if err := d.waitForJobEvents(d.NewReleaseID, expected, nlog); err != nil {
		nlog.Error("error waiting for job events", "err", err)
		return err
	}

	olog.Info("scaling old formation to zero")
	if err := d.client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.OldReleaseID,
	}); err != nil {
		log.Error("error scaling old formation to zero", "err", err)
		return err
	}

	expected = make(jobEvents)
	for typ, n := range f.Processes {
		for i := 0; i < n; i++ {
			d.deployEvents <- ct.DeploymentEvent{
				ReleaseID: d.OldReleaseID,
				JobState:  "stopping",
				JobType:   typ,
			}
		}
		expected[typ] = map[string]int{"down": n}
	}
	olog.Info("waiting for job events", "expected", expected)
	if err := d.waitForJobEvents(d.OldReleaseID, expected, olog); err != nil {
		olog.Error("error waiting for job events", "err", err)
		return err
	}
	log.Info("finished all-at-once deployment")
	return nil
}
