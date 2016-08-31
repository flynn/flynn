package deployment

import (
	"fmt"
	"sort"

	ct "github.com/flynn/flynn/controller/types"
	"gopkg.in/inconshreveable/log15.v2"
)

type WaitJobsFn func(releaseID string, expected ct.JobEvents, log log15.Logger) error

func (d *DeployJob) deployOneByOne() error {
	return d.deployOneByOneWithWaitFn(d.waitForJobEvents)
}

func (d *DeployJob) deployOneByOneWithWaitFn(waitJobs WaitJobsFn) error {
	log := d.logger.New("fn", "deployOneByOne")
	log.Info("starting one-by-one deployment")

	oldScale := make(map[string]int, len(d.oldReleaseState))
	for typ, count := range d.oldReleaseState {
		oldScale[typ] = count
		if d.isOmni(typ) {
			oldScale[typ] /= d.hostCount
		}
	}

	newScale := make(map[string]int, len(d.newReleaseState))
	for typ, count := range d.newReleaseState {
		newScale[typ] = count
		if d.isOmni(typ) {
			newScale[typ] /= d.hostCount
		}
	}

	processTypes := make([]string, 0, len(d.Processes))
	for typ := range d.Processes {
		processTypes = append(processTypes, typ)
	}
	sort.Sort(sort.StringSlice(processTypes))

	olog := log.New("release_id", d.OldReleaseID)
	nlog := log.New("release_id", d.NewReleaseID)
	for _, typ := range processTypes {
		num := d.Processes[typ]
		// don't scale processes which no longer exist in the new release
		if _, ok := d.newRelease.Processes[typ]; !ok {
			num = 0
		}
		diff := 1
		if d.isOmni(typ) {
			diff = d.hostCount
		}

		for i := newScale[typ]; i < num; i++ {
			nlog.Info("scaling new formation up by one", "type", typ)
			newScale[typ]++
			if err := d.client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.NewReleaseID,
				Processes: newScale,
			}); err != nil {
				nlog.Error("error scaling new formation up by one", "type", typ, "err", err)
				return err
			}
			for i := 0; i < diff; i++ {
				d.deployEvents <- ct.DeploymentEvent{
					ReleaseID: d.NewReleaseID,
					JobState:  ct.JobStateStarting,
					JobType:   typ,
				}
			}
			nlog.Info(fmt.Sprintf("waiting for %d job up event(s)", diff), "type", typ)
			if err := waitJobs(d.NewReleaseID, ct.JobEvents{typ: ct.JobUpEvents(diff)}, nlog); err != nil {
				nlog.Error("error waiting for job up events", "err", err)
				return err
			}

			olog.Info("scaling old formation down by one", "type", typ)
			oldScale[typ]--
			if err := d.client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.OldReleaseID,
				Processes: oldScale,
			}); err != nil {
				olog.Error("error scaling old formation down by one", "type", typ, "err", err)
				return err
			}
			for i := 0; i < diff; i++ {
				d.deployEvents <- ct.DeploymentEvent{
					ReleaseID: d.OldReleaseID,
					JobState:  ct.JobStateStopping,
					JobType:   typ,
				}
			}

			olog.Info(fmt.Sprintf("waiting for %d job down event(s)", diff), "type", typ)
			if err := waitJobs(d.OldReleaseID, ct.JobEvents{typ: ct.JobDownEvents(diff)}, olog); err != nil {
				olog.Error("error waiting for job down events", "err", err)
				return err
			}
		}
	}

	// ensure any old leftover jobs are stopped (this can happen when new
	// workers continue deployments from old workers and still see the
	// old worker running even though it has been scaled down), returning
	// ErrSkipRollback if an error occurs (rolling back doesn't make a ton
	// of sense because it involves stopping the new working jobs).
	log.Info("ensuring old formation is scaled down to zero")
	diff := make(ct.JobEvents, len(oldScale))
	for typ, count := range oldScale {
		if count > 0 {
			diff[typ] = ct.JobDownEvents(count)
		}
	}
	if err := d.client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.OldReleaseID,
	}); err != nil {
		log.Error("error scaling old formation down to zero", "err", err)
		return ErrSkipRollback{err.Error()}
	}
	if diff.Count() > 0 {
		log.Info(fmt.Sprintf("waiting for %d job down event(s)", diff.Count()), "diff", diff)
		if err := d.waitForJobEvents(d.OldReleaseID, diff, log); err != nil {
			log.Error("error waiting for job down events", "diff", diff, "err", err)
			return ErrSkipRollback{err.Error()}
		}
	}

	log.Info("finished one-by-one deployment")
	return nil
}
