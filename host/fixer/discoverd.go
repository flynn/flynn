package fixer

import (
	"fmt"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

func (f *ClusterFixer) FixDiscoverd() error {
	f.l.Info("ensuring discoverd is running on all hosts")
	releases := f.FindAppReleaseJobs("discoverd", "app")
	if len(releases) == 0 {
		return fmt.Errorf("didn't find any discoverd release jobs")
	}
outer:
	for hostID, job := range releases[0] {
		for _, h := range f.hosts {
			if h.ID() != hostID {
				continue
			}

			// check if discoverd is already running on this host
			jobs, err := h.ListJobs()
			if err != nil {
				return fmt.Errorf("error listing jobs on %s: %s", h.ID(), err)
			}
			for _, j := range jobs {
				if j.Status == host.StatusRunning &&
					j.Job.Metadata["flynn-controller.app_name"] == "discoverd" &&
					j.Job.Metadata["flynn-controller.type"] == "app" {
					continue outer
				}
			}

			job.ID = cluster.GenerateJobID(h.ID(), "")
			f.FixJobEnv(job)
			if err := h.AddJob(job); err != nil {
				return fmt.Errorf("error starting discoverd on %s: %s", h.ID(), err)
			}
			f.l.Info("started discoverd instance", "job.id", job.ID)
			break
		}
	}
	return nil
}
