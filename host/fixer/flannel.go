package fixer

import (
	"fmt"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

func (f *ClusterFixer) FixFlannel() error {
	f.l.Info("checking flannel")

	flannelJobs := make(map[string]*host.Job, len(f.hosts))
	for _, h := range f.hosts {
		jobs, err := h.ListJobs()
		if err != nil {
			return fmt.Errorf("error getting jobs list from %s: %s", h.ID(), err)
		}
		for _, j := range jobs {
			if j.Status != host.StatusRunning ||
				j.Job.Metadata["flynn-controller.app_name"] != "flannel" ||
				j.Job.Metadata["flynn-controller.type"] != "app" {
				continue
			}
			flannelJobs[h.ID()] = j.Job
			break
		}
	}
	if len(flannelJobs) == len(f.hosts) {
		f.l.Info("flannel looks good")
		return nil
	}

	var job *host.Job
	if len(flannelJobs) == 0 {
		f.l.Info("flannel not running, starting it on each host")
		releases := f.FindAppReleaseJobs("flannel", "app")
		if len(releases) == 0 {
			return fmt.Errorf("didn't find flannel release jobs")
		}
		for _, j := range releases[0] {
			job = j
			break
		}
	} else {
		f.l.Info("flannel is not running on each host, starting missing jobs")
		for _, job = range flannelJobs {
			break
		}
	}

	for _, h := range f.hosts {
		if _, ok := flannelJobs[h.ID()]; ok {
			continue
		}
		job.ID = cluster.GenerateJobID(h.ID(), "")
		f.FixJobEnv(job)
		if err := h.AddJob(job); err != nil {
			return fmt.Errorf("error starting flannel job: %s", err)
		}
		f.l.Info("started flannel job", "job.id", job.ID)
	}

	f.l.Info("flannel fix complete")

	return nil
}
