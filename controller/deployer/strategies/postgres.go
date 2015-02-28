package strategy

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flynn/flynn/appliance/postgresql/client"
	"github.com/flynn/flynn/appliance/postgresql/state"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
)

func postgres(d *Deploy) error {
	log := d.logger.New("fn", "postgres")
	log.Info("starting postgres deployment")

	loggedErr := func(e string) error {
		log.Error(e)
		return errors.New(e)
	}

	if d.serviceMeta == nil {
		return loggedErr("missing pg cluster state")
	}

	var state state.State
	log.Info("decoding pg cluster state")
	if err := json.Unmarshal(d.serviceMeta.Data, &state); err != nil {
		log.Error("error decoding pg cluster state", "err", err)
		return err
	}

	// abort if in singleton mode or not deploying from a clean state
	if state.Singleton {
		return loggedErr("pg cluster in singleton mode")
	}
	if len(state.Async) == 0 {
		return loggedErr("pg cluster in unhealthy state (has no asyncs)")
	}
	if 2+len(state.Async) != d.Processes["postgres"] {
		return loggedErr(fmt.Sprintf("pg cluster in unhealthy state (too few asyncs)"))
	}
	if d.newReleaseState["postgres"] > 0 {
		return loggedErr("pg cluster in unexpected state")
	}

	stopInstance := func(inst *discoverd.Instance) error {
		log := log.New("job_id", inst.Meta["FLYNN_JOB_ID"])

		d.deployEvents <- ct.DeploymentEvent{
			ReleaseID: d.OldReleaseID,
			JobState:  "stopping",
			JobType:   "postgres",
		}
		pg := pgmanager.NewClient(inst.Addr)
		log.Info("stopping postgres")
		if err := pg.Stop(); err != nil {
			log.Error("error stopping postgres", "err", err)
			return err
		}
		log.Info("waiting for postgres to stop")
		for {
			select {
			case event := <-d.serviceEvents:
				if event.Kind == discoverd.EventKindDown && event.Instance.ID == inst.ID {
					d.deployEvents <- ct.DeploymentEvent{
						ReleaseID: d.OldReleaseID,
						JobState:  "down",
						JobType:   "postgres",
					}
					return nil
				}
			case <-time.After(60 * time.Second):
				return loggedErr("timed out waiting for postgres to stop")
			}
		}
	}

	// newPrimary is the first new instance started, newSync the second
	var newPrimary, newSync *discoverd.Instance
	startInstance := func() (*discoverd.Instance, error) {
		log.Info("starting new instance")
		d.deployEvents <- ct.DeploymentEvent{
			ReleaseID: d.NewReleaseID,
			JobState:  "starting",
			JobType:   "postgres",
		}
		d.newReleaseState["postgres"]++
		if err := d.client.PutFormation(&ct.Formation{
			AppID:     d.AppID,
			ReleaseID: d.NewReleaseID,
			Processes: d.newReleaseState,
		}); err != nil {
			log.Error("error scaling postgres formation up by one", "err", err)
			return nil, err
		}
		log.Info("waiting for new instance to come up")
		var inst *discoverd.Instance
	loop:
		for {
			select {
			case event := <-d.serviceEvents:
				if event.Kind == discoverd.EventKindUp &&
					event.Instance.Meta != nil &&
					event.Instance.Meta["FLYNN_RELEASE_ID"] == d.NewReleaseID &&
					event.Instance.Meta["FLYNN_PROCESS_TYPE"] == "postgres" {
					inst = event.Instance
					break loop
				}
			case <-time.After(60 * time.Second):
				return nil, loggedErr("timed out waiting for new instance to come up")
			}
		}
		if newPrimary == nil {
			newPrimary = inst
		} else if newSync == nil {
			newSync = inst
		}
		return inst, nil
	}
	waitForSync := func(upstream, downstream *discoverd.Instance) error {
		log.Info("waiting for replication sync", "upstream", upstream.Addr, "downstream", downstream.Addr)
		client := pgmanager.NewClient(upstream.Addr)
		if err := client.WaitForReplSync(downstream, 3*time.Minute); err != nil {
			log.Error("error waiting for replication sync", "err", err)
			return err
		}
		d.deployEvents <- ct.DeploymentEvent{
			ReleaseID: d.NewReleaseID,
			JobState:  "up",
			JobType:   "postgres",
		}
		return nil
	}

	// asyncUpstream is the instance we will query for replication status
	// of the new async, which will be the sync if there is only one
	// async, or the tail async otherwise.
	asyncUpstream := state.Sync
	if len(state.Async) > 1 {
		asyncUpstream = state.Async[len(state.Async)-1]
	}
	for i := 0; i < len(state.Async); i++ {
		log.Info("replacing an Async node")
		if err := stopInstance(state.Async[i]); err != nil {
			return err
		}
		newInst, err := startInstance()
		if err != nil {
			return err
		}
		if err := waitForSync(asyncUpstream, newInst); err != nil {
			return err
		}
		// the new instance is now the tail async
		asyncUpstream = newInst
	}

	log.Info("replacing the Sync node")
	if err := stopInstance(state.Sync); err != nil {
		return err
	}
	_, err := startInstance()
	if err != nil {
		return err
	}
	if err := waitForSync(state.Primary, newPrimary); err != nil {
		return err
	}

	log.Info("replacing the Primary node")
	if err := stopInstance(state.Primary); err != nil {
		return err
	}
	_, err = startInstance()
	if err != nil {
		return err
	}
	if err := waitForSync(newPrimary, newSync); err != nil {
		return err
	}

	log.Info("stopping old postgres jobs")
	d.oldReleaseState["postgres"] = 0
	if err := d.client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.OldReleaseID,
		Processes: d.oldReleaseState,
	}); err != nil {
		log.Error("error scaling old formation", "err", err)
		return err
	}

	log.Info(fmt.Sprintf("waiting for %d job down events", d.Processes["postgres"]))
	actual := 0
loop:
	for {
		select {
		case event, ok := <-d.jobEvents:
			if !ok {
				return loggedErr("unexpected close of job event stream")
			}
			log.Info("got job event", "job_id", event.JobID, "type", event.Type, "state", event.State)
			if event.State == "down" && event.Type == "postgres" && event.Job.ReleaseID == d.OldReleaseID {
				actual++
				if actual == d.Processes["postgres"] {
					break loop
				}
			}
		case <-time.After(60 * time.Second):
			return loggedErr("timed out waiting for job events")
		}
	}

	// do a one-by-one deploy for the other process types
	return oneByOne(d)
}
