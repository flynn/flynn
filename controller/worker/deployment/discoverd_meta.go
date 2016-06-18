package deployment

import (
	ct "github.com/flynn/flynn/controller/types"
	dd "github.com/flynn/flynn/discoverd/deployment"
	"gopkg.in/inconshreveable/log15.v2"
)

// deployDiscoverMeta does a one-by-one deployment but uses discoverd.Deployment
// to wait for appropriate service metadata before stopping old jobs.
func (d *DeployJob) deployDiscoverdMeta() (err error) {
	log := d.logger.New("fn", "deployDiscoverdMeta")
	log.Info("starting discoverd-meta deployment")

	defer func() {
		if err != nil {
			// TODO: support rolling back
			err = ErrSkipRollback{err.Error()}
		}
	}()

	discDeploys := make(map[string]*dd.Deployment)

	for typ, serviceName := range d.serviceNames {
		discDeploy, err := dd.NewDeployment(serviceName)
		if err != nil {
			return err
		}
		discDeploys[typ] = discDeploy
		if err := discDeploy.Create(d.ID); err != nil {
			return err
		}
		defer discDeploy.Close()
	}

	return d.deployOneByOneWithWaitFn(func(releaseID string, expected ct.JobEvents, log log15.Logger) error {
		for typ, events := range expected {
			if count, ok := events[ct.JobStateUp]; ok && count > 0 {
				if discDeploy, ok := discDeploys[typ]; ok {
					if err := discDeploy.Wait(d.ID, count, 120, log); err != nil {
						return err
					}
					// clear up events for this type so we can safely
					// process job down events if needed
					expected[typ][ct.JobStateUp] = 0
				}
			}
		}
		if expected.Count() == 0 {
			return nil
		}
		return d.waitForJobEvents(releaseID, expected, log)
	})
}
