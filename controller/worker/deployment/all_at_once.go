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
		if existing := d.oldReleaseState[typ]; existing > 0 {
			expected[typ] = ct.JobDownEvents(existing)
		}
	}

	log = log.New("release_id", d.OldReleaseID)
	log.Info("scaling old formation to zero")
	if err := d.client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.OldReleaseID,
	}); err != nil {
		// the new jobs have now started and they are up, so return
		// ErrSkipRollback (rolling back doesn't make a ton of sense
		// because it involves stopping the new working jobs).
		log.Error("error scaling old formation to zero", "err", err)
		return ErrSkipRollback{err.Error()}
	}

	// treat the deployment as finished now (rather than waiting for the
	// jobs to actually stop) as we can trust that the scheduler will
	// actually kill the jobs, so no need to delay the deployment.
	log.Info("finished all-at-once deployment")
	return nil
}
