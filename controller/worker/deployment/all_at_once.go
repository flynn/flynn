package deployment

import ct "github.com/flynn/flynn/controller/types"

func (d *DeployJob) deployAllAtOnce() error {
	log := d.logger.New("fn", "deployAllAtOnce")
	log.Info("starting all-at-once deployment")

	expected := make(ct.JobEvents)
	newProcs := make(map[string]int, len(d.Processes))
	for typ, n := range d.Processes {
		// ignore processes which no longer exist in the new
		// release
		if _, ok := d.newRelease.Processes[typ]; !ok {
			continue
		}
		newProcs[typ] = n
		total := n
		if d.isOmni(typ) {
			total *= d.hostCount
		}
		existing := d.newReleaseState[typ]
		for i := existing; i < total; i++ {
			d.deployEvents <- ct.DeploymentEvent{
				ReleaseID: d.NewReleaseID,
				JobState:  ct.JobStateStarting,
				JobType:   typ,
			}
		}
		if total > existing {
			expected[typ] = ct.JobUpEvents(total - existing)
		}
	}
	if expected.Count() > 0 {
		log := log.New("release_id", d.NewReleaseID)
		log.Info("creating new formation", "processes", newProcs)
		if err := d.client.PutFormation(&ct.Formation{
			AppID:     d.AppID,
			ReleaseID: d.NewReleaseID,
			Processes: newProcs,
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

	expected = make(ct.JobEvents)
	for typ := range d.Processes {
		existing := d.oldReleaseState[typ]
		for i := 0; i < existing; i++ {
			d.deployEvents <- ct.DeploymentEvent{
				ReleaseID: d.OldReleaseID,
				JobState:  ct.JobStateStopping,
				JobType:   typ,
			}
		}
		if existing > 0 {
			expected[typ] = ct.JobDownEvents(existing)
		}
	}

	// the new jobs have now started and they are up, so return
	// ErrSkipRollback from here on out if an error occurs (rolling
	// back doesn't make a ton of sense because it involves
	// stopping the new working jobs).
	log = log.New("release_id", d.OldReleaseID)
	log.Info("scaling old formation to zero")
	if err := d.client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.OldReleaseID,
	}); err != nil {
		log.Error("error scaling old formation to zero", "err", err)
		return ErrSkipRollback{err.Error()}
	}

	if expected.Count() > 0 {
		log.Info("waiting for job events", "expected", expected)
		if err := d.waitForJobEvents(d.OldReleaseID, expected, log); err != nil {
			log.Error("error waiting for job events", "err", err)
			return ErrSkipRollback{err.Error()}
		}
	}

	log.Info("finished all-at-once deployment")
	return nil
}
