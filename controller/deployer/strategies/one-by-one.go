package strategy

import (
	"fmt"

	ct "github.com/flynn/flynn/controller/types"
)

func oneByOne(d *Deploy) error {
	log := d.logger.New("fn", "oneByOne")
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

	olog := log.New("release_id", d.OldReleaseID)
	nlog := log.New("release_id", d.NewReleaseID)
	for typ, num := range d.Processes {
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
					JobState:  "starting",
					JobType:   typ,
				}
			}
			nlog.Info(fmt.Sprintf("waiting for %d job up event(s)", diff), "type", typ)
			if err := d.waitForJobEvents(d.NewReleaseID, jobEvents{typ: {"up": diff}}, nlog); err != nil {
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
					JobState:  "stopping",
					JobType:   typ,
				}
			}

			olog.Info(fmt.Sprintf("waiting for %d job down event(s)", diff), "type", typ)
			if err := d.waitForJobEvents(d.OldReleaseID, jobEvents{typ: {"down": diff}}, olog); err != nil {
				olog.Error("error waiting for job down events", "err", err)
				return err
			}
		}
	}
	log.Info("finished one-by-one deployment")
	return nil
}
