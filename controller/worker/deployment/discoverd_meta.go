package deployment

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	dd "github.com/flynn/flynn/discoverd/deployment"
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
		if err := discDeploy.Reset(); err != nil {
			return err
		}
		defer discDeploy.Close()
	}

	return d.deployOneByOneWithWaitFn(func(releaseID string, expected jobEvents, log log15.Logger) error {
		for typ, events := range expected {
			if count, ok := events["up"]; ok && count > 0 {
				if discDeploy, ok := discDeploys[typ]; ok {
					if err := discDeploy.Wait(count, 120, log); err != nil {
						return err
					}
					// clear up events for this type so we can safely
					// process job down events if needed
					expected[typ]["up"] = 0
				}
			}
		}
		return d.waitForJobEvents(releaseID, expected, log)
	})
}
