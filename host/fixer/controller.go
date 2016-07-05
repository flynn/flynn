package fixer

import (
	"fmt"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
)

func (f *ClusterFixer) FixController(instances []*discoverd.Instance, startScheduler bool) error {
	f.l.Info("found controller instance, checking critical formations")
	inst := instances[0]
	client, err := controller.NewClient("http://"+inst.Addr, inst.Meta["AUTH_KEY"])
	if err != nil {
		return fmt.Errorf("unexpected error creating controller client: %s", err)
	}

	// check that formations for critical components are expected
	apps := []string{"controller", "router", "discoverd", "flannel", "postgres"}
	changes := make(map[string]*ct.Formation, len(apps))
	var controllerFormation *ct.Formation
	for _, app := range apps {
		release, err := client.GetAppRelease(app)
		if err != nil {
			return fmt.Errorf("error getting %s release: %s", app, err)
		}
		formation, err := client.GetFormation(app, release.ID)
		if err != nil {
			// TODO: handle ErrNotFound
			return fmt.Errorf("error getting %s formation: %s", app, err)
		}
		if app == "controller" {
			controllerFormation = formation
		}
		for typ := range release.Processes {
			var want int
			if app == "postgres" && typ == "postgres" && len(f.hosts) > 1 && formation.Processes[typ] < 3 {
				want = 3
			} else if formation.Processes[typ] < 1 {
				want = 1
			}
			if want > 0 {
				f.l.Info("found broken formation", "app", app, "process", typ)
				if _, ok := changes[app]; !ok {
					if formation.Processes == nil {
						formation.Processes = make(map[string]int)
					}
					changes[app] = formation
				}
				changes[app].Processes[typ] = want
			}
		}
	}

	for app, formation := range changes {
		f.l.Info("fixing broken formation", "app", app)
		if err := client.PutFormation(formation); err != nil {
			return fmt.Errorf("error putting %s formation: %s", app, err)
		}
	}

	if startScheduler {
		if err := f.StartScheduler(client, controllerFormation); err != nil {
			return err
		}
	}
	return nil
}

func (f *ClusterFixer) StartScheduler(client controller.Client, cf *ct.Formation) error {
	if _, err := discoverd.NewService("controller-scheduler").Leader(); err != nil && !discoverd.IsNotFound(err) {
		return fmt.Errorf("error getting scheduler leader: %s", err)
	} else if err == nil {
		f.l.Info("scheduler looks up, moving on")
		return nil
	}
	f.l.Info("scheduler is not up, attempting to fix")

	// start scheduler
	ef, err := utils.ExpandFormation(client, cf)
	if err != nil {
		return err
	}
	schedulerJob := utils.JobConfig(ef, "scheduler", f.hosts[0].ID(), "")
	if err := f.hosts[0].AddJob(schedulerJob); err != nil {
		return fmt.Errorf("error starting scheduler job on %s: %s", f.hosts[0].ID(), err)
	}
	f.l.Info("started scheduler job")
	return nil
}

func (f *ClusterFixer) KillSchedulers() error {
	f.l.Info("killing any running schedulers to prevent interference")
	for _, h := range f.hosts {
		jobs, err := h.ListJobs()
		if err != nil {
			return fmt.Errorf("error listing jobs from %s: %s", h.ID(), err)
		}
		for _, j := range jobs {
			if j.Job.Metadata["flynn-controller.app_name"] != "controller" || j.Job.Metadata["flynn-controller.type"] != "scheduler" {
				continue
			}
			if j.Status != host.StatusRunning && j.Status != host.StatusStarting {
				continue
			}
			if err := h.StopJob(j.Job.ID); err != nil {
				f.l.Error("error stopping scheduler job", "id", j.Job.ID, "error", err)
			}
			f.l.Info("stopped scheduler instance", "job.id", j.Job.ID)
		}
	}
	return nil
}
