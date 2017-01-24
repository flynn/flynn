package deployment

import (
	"fmt"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/worker/types"
	"gopkg.in/inconshreveable/log15.v2"
)

type DeployJob struct {
	*ct.Deployment
	client       controller.Client
	deployEvents chan<- ct.DeploymentEvent
	logger       log15.Logger
	oldRelease   *ct.Release
	newRelease   *ct.Release
	oldFormation *ct.Formation
	newFormation *ct.Formation
	timeout      time.Duration
	stop         chan struct{}
}

func (d *DeployJob) Perform() error {
	log := d.logger.New("fn", "Perform", "deployment_id", d.ID, "app_id", d.AppID)

	log.Info("validating deployment strategy")
	var deployFunc func() error
	switch d.Strategy {
	case "one-by-one":
		deployFunc = d.deployOneByOne
	case "all-at-once":
		deployFunc = d.deployAllAtOnce
	case "sirenia":
		deployFunc = d.deploySirenia
	case "discoverd-meta":
		deployFunc = d.deployDiscoverdMeta
	default:
		err := UnknownStrategyError{d.Strategy}
		log.Error("error validating deployment strategy", "err", err)
		return err
	}

	log.Info("getting old release", "release.id", d.OldReleaseID)
	var err error
	d.oldRelease, err = d.client.GetRelease(d.OldReleaseID)
	if err != nil {
		log.Error("error getting old release", "release.id", d.OldReleaseID, "err", err)
		return err
	}
	d.oldFormation, err = d.client.GetFormation(d.AppID, d.OldReleaseID)
	if err != nil {
		log.Error("error getting old formation", "release.id", d.OldReleaseID, "err", err)
		return err
	}

	log.Info("getting new release", "release.id", d.NewReleaseID)
	d.newRelease, err = d.client.GetRelease(d.NewReleaseID)
	if err != nil {
		log.Error("error getting new release", "release.id", d.NewReleaseID, "err", err)
		return err
	}
	d.newFormation, err = d.client.GetFormation(d.AppID, d.NewReleaseID)
	if err == controller.ErrNotFound {
		d.newFormation = &ct.Formation{
			AppID:     d.AppID,
			ReleaseID: d.NewReleaseID,
			Tags:      d.Tags,
		}
	} else if err != nil {
		return err
	}
	if d.newFormation.Processes == nil {
		d.newFormation.Processes = make(map[string]int)
	}

	if processesEqual(d.newFormation.Processes, d.Processes) {
		log.Info("deployment already completed, nothing to do")
		return nil
	}

	d.timeout = time.Duration(d.DeployTimeout) * time.Second

	log.Info(
		"determined deployment state",
		"original", d.Processes,
		"old_release", d.oldFormation.Processes,
		"new_release", d.newFormation.Processes,
	)
	return deployFunc()
}

func (d *DeployJob) scaleOldRelease(wait bool) error {
	opts := ct.ScaleOptions{
		Processes:        d.oldFormation.Processes,
		Timeout:          &d.timeout,
		Stop:             d.stop,
		NoWait:           !wait,
		JobEventCallback: d.logJobEvent,
	}
	err := d.client.ScaleAppRelease(d.AppID, d.OldReleaseID, opts)
	if err == ct.ErrScalingStopped {
		err = worker.ErrStopped
	}
	return err
}

// failedJobThreshold is the number of times new jobs can fail when scaling up
// a new release before aborting the deploy
const newJobFailureThreshold = 5

func (d *DeployJob) scaleNewRelease() error {
	failures := 0
	opts := ct.ScaleOptions{
		Processes: d.newFormation.Processes,
		Tags:      d.newFormation.Tags,
		Timeout:   &d.timeout,
		Stop:      d.stop,
		JobEventCallback: func(job *ct.Job) error {
			d.logJobEvent(job)
			// return an error if we get more than newJobFailureThreshold
			// down events when scaling the new formation up
			if job.State == ct.JobStateDown {
				failures++
				if failures <= newJobFailureThreshold {
					d.logger.Warn("ignoring down job event for new release", "count", failures, "err", job.HostError)
					return nil
				}
				msg := "got down job event"
				if job.HostError != nil {
					msg = *job.HostError
				}
				return fmt.Errorf("%s job failed to start: %s", job.Type, msg)
			}
			return nil
		},
	}
	err := d.client.ScaleAppRelease(d.AppID, d.NewReleaseID, opts)
	if err == ct.ErrScalingStopped {
		err = worker.ErrStopped
	}
	return err
}

func (d *DeployJob) logJobEvent(job *ct.Job) error {
	d.logger.Info(
		"got job event",
		"release.id", job.ReleaseID,
		"job.id", job.ID,
		"job.type", job.Type,
		"job.state", job.State,
	)
	return nil
}

func (d *DeployJob) scaleOneByOne(typ string, log log15.Logger) error {
	for i := 0; i < d.Processes[typ]; i++ {
		if err := d.scaleNewFormationUpByOne(typ, log); err != nil {
			return err
		}

		if err := d.scaleOldFormationDownByOne(typ, log); err != nil {
			return err
		}
	}
	return nil
}

func (d *DeployJob) scaleNewFormationUpByOne(typ string, log log15.Logger) error {
	// only scale new processes which still exist
	if _, ok := d.newRelease.Processes[typ]; !ok {
		return nil
	}
	// don't scale higher than d.Processes
	if d.newFormation.Processes[typ] == d.Processes[typ] {
		return nil
	}
	log.Info("scaling new formation up by one", "release.id", d.NewReleaseID, "job.type", typ)
	d.newFormation.Processes[typ]++
	if err := d.scaleNewRelease(); err != nil {
		log.Error("error scaling new formation up by one", "release.id", d.NewReleaseID, "job.type", typ, "err", err)
		return err
	}
	return nil
}

func (d *DeployJob) scaleOldFormationDownByOne(typ string, log log15.Logger) error {
	// don't scale lower than zero
	if d.oldFormation.Processes[typ] == 0 {
		return nil
	}
	log.Info("scaling old formation down by one", "release.id", d.OldReleaseID, "job.type", typ)
	d.oldFormation.Processes[typ]--
	if err := d.scaleOldRelease(true); err != nil {
		log.Error("error scaling old formation down by one", "release.id", d.OldReleaseID, "job.type", typ, "err", err)
		return err
	}
	return nil
}

func processesEqual(a map[string]int, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for typ, countA := range a {
		if countB, ok := b[typ]; !ok || countA != countB {
			return false
		}
	}
	return true
}
