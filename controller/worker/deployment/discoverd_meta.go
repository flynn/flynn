package deployment

import dd "github.com/flynn/flynn/discoverd/deployment"

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

	for typ, count := range d.Processes {
		proc := d.newRelease.Processes[typ]

		if proc.Service == "" {
			if err := d.scaleOneByOne(typ, log); err != nil {
				return err
			}
			continue
		}

		discDeploy, err := dd.NewDeployment(proc.Service)
		if err != nil {
			return err
		}
		if err := discDeploy.Create(d.ID); err != nil {
			return err
		}
		defer discDeploy.Close()

		for i := 0; i < count; i++ {
			if err := d.scaleNewFormationUpByOne(typ, log); err != nil {
				return err
			}
			if err := discDeploy.Wait(d.ID, d.timeout, log); err != nil {
				return err
			}
			if err := d.scaleOldFormationDownByOne(typ, log); err != nil {
				return err
			}
		}
	}
	return nil
}
