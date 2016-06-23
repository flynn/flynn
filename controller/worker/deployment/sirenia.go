package deployment

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/sirenia/client"
	"github.com/flynn/flynn/pkg/sirenia/state"
)

func (d *DeployJob) deploySirenia() (err error) {
	log := d.logger.New("fn", "deploySirenia")
	log.Info("starting sirenia deployment")

	defer func() {
		if err != nil {
			err = ErrSkipRollback{err.Error()}
		}
	}()

	loggedErr := func(e string) error {
		log.Error(e)
		return errors.New(e)
	}

	processType := d.oldRelease.Env["SIRENIA_PROCESS"]
	// if the process type isn't set try getting it from the new release
	if processType == "" {
		processType = d.newRelease.Env["SIRENIA_PROCESS"]
	}
	// if it's still not set we have a problem.
	if processType == "" {
		return fmt.Errorf("unable to determine sirenia process type")
	}

	// if sirenia process type is scaled to 0, skip and deploy non-sirenia processes
	if d.Processes[processType] == 0 {
		log.Info("sirenia process type scale = 0, skipping")
		return d.deployOneByOne()
	}

	if d.serviceMeta == nil {
		return loggedErr("missing sirenia cluster state")
	}

	var state state.State
	log.Info("decoding sirenia cluster state")
	if err := json.Unmarshal(d.serviceMeta.Data, &state); err != nil {
		log.Error("error decoding sirenia cluster state", "err", err)
		return err
	}

	// abort if in singleton mode or not deploying from a clean state
	if state.Singleton {
		return loggedErr("sirenia cluster in singleton mode")
	}
	if len(state.Async) == 0 {
		return loggedErr("sirenia cluster in unhealthy state (has no asyncs)")
	}
	if 2+len(state.Async) != d.Processes[processType] {
		return loggedErr(fmt.Sprintf("sirenia cluster in unhealthy state (too few asyncs)"))
	}
	if processesEqual(d.newReleaseState, d.Processes) {
		log.Info("deployment already completed, nothing to do")
		return nil
	}
	if d.newReleaseState[processType] > 0 {
		return loggedErr("sirenia cluster in unexpected state")
	}

	stopInstance := func(inst *discoverd.Instance) error {
		log := log.New("job_id", inst.Meta["FLYNN_JOB_ID"])

		d.deployEvents <- ct.DeploymentEvent{
			ReleaseID: d.OldReleaseID,
			JobState:  ct.JobStateStopping,
			JobType:   processType,
		}
		peer := client.NewClient(inst.Addr)
		log.Info("stopping peer")
		if err := peer.Stop(); err != nil {
			log.Error("error stopping peer", "err", err)
			return err
		}
		log.Info("waiting for peer to stop")
		jobEvents := d.ReleaseJobEvents(d.OldReleaseID)
		for {
			select {
			case e := <-jobEvents:
				if e.Type == JobEventTypeError {
					return e.Error
				}
				if e.Type != JobEventTypeDiscoverd {
					continue
				}
				event := e.DiscoverdEvent
				if event.Kind == discoverd.EventKindDown && event.Instance.ID == inst.ID {
					d.deployEvents <- ct.DeploymentEvent{
						ReleaseID: d.OldReleaseID,
						JobState:  ct.JobStateDown,
						JobType:   processType,
					}
					return nil
				}
			case <-time.After(time.Duration(d.DeployTimeout) * time.Second):
				return loggedErr("timed out waiting for peer to stop")
			}
		}
	}

	// newPrimary is the first new instance started, newSync the second
	var newPrimary, newSync *discoverd.Instance
	startInstance := func() (*discoverd.Instance, error) {
		log.Info("starting new instance")
		d.deployEvents <- ct.DeploymentEvent{
			ReleaseID: d.NewReleaseID,
			JobState:  ct.JobStateStarting,
			JobType:   processType,
		}
		d.newReleaseState[processType]++
		if err := d.client.PutFormation(&ct.Formation{
			AppID:     d.AppID,
			ReleaseID: d.NewReleaseID,
			Processes: d.newReleaseState,
		}); err != nil {
			log.Error("error scaling formation up by one", "err", err)
			return nil, err
		}
		log.Info("waiting for new instance to come up")
		var inst *discoverd.Instance
		jobEvents := d.ReleaseJobEvents(d.NewReleaseID)
	loop:
		for {
			select {
			case e := <-jobEvents:
				if e.Type == JobEventTypeError {
					return nil, e.Error
				}
				if e.Type != JobEventTypeDiscoverd {
					continue
				}
				event := e.DiscoverdEvent
				if event.Kind == discoverd.EventKindUp &&
					event.Instance.Meta != nil &&
					event.Instance.Meta["FLYNN_RELEASE_ID"] == d.NewReleaseID &&
					event.Instance.Meta["FLYNN_PROCESS_TYPE"] == processType {
					inst = event.Instance
					break loop
				}
			case <-time.After(time.Duration(d.DeployTimeout) * time.Second):
				return nil, loggedErr("timed out waiting for new instance to come up")
			}
		}
		if newPrimary == nil {
			newPrimary = inst
		} else if newSync == nil {
			newSync = inst
		}
		d.deployEvents <- ct.DeploymentEvent{
			ReleaseID: d.NewReleaseID,
			JobState:  ct.JobStateUp,
			JobType:   processType,
		}
		return inst, nil
	}
	waitForSync := func(upstream, downstream *discoverd.Instance) error {
		log.Info("waiting for replication sync", "upstream", upstream.Addr, "downstream", downstream.Addr)
		client := client.NewClient(upstream.Addr)
		if err := client.WaitForReplSync(downstream, 3*time.Minute); err != nil {
			log.Error("error waiting for replication sync", "err", err)
			return err
		}
		return nil
	}
	waitForReadWrite := func(inst *discoverd.Instance) error {
		log.Info("waiting for read-write", "inst", inst.Addr)
		client := client.NewClient(inst.Addr)
		if err := client.WaitForReadWrite(3 * time.Minute); err != nil {
			log.Error("error waiting for read-write", "err", err)
			return err
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
		newInst, err := startInstance()
		if err != nil {
			return err
		}
		if err := stopInstance(state.Async[i]); err != nil {
			return err
		}
		if err := waitForSync(asyncUpstream, newInst); err != nil {
			return err
		}
		// the new instance is now the tail async
		asyncUpstream = newInst
	}

	log.Info("replacing the Sync node")
	_, err = startInstance()
	if err != nil {
		return err
	}
	if err := stopInstance(state.Sync); err != nil {
		return err
	}
	if err := waitForSync(state.Primary, newPrimary); err != nil {
		return err
	}

	// wait for the new Sync to catch the new Primary *before* killing the
	// old Primary to avoid backups failing
	if err := waitForSync(newPrimary, newSync); err != nil {
		return err
	}

	log.Info("replacing the Primary node")
	_, err = startInstance()
	if err != nil {
		return err
	}
	if err := stopInstance(state.Primary); err != nil {
		return err
	}
	if err := waitForReadWrite(newPrimary); err != nil {
		return err
	}

	log.Info("stopping old jobs")
	d.oldReleaseState[processType] = 0
	if err := d.client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.OldReleaseID,
		Processes: d.oldReleaseState,
	}); err != nil {
		log.Error("error scaling old formation", "err", err)
		return err
	}

	log.Info(fmt.Sprintf("waiting for %d job down events", d.Processes[processType]))
	actual := 0
	jobEvents := d.ReleaseJobEvents(d.OldReleaseID)
loop:
	for {
		select {
		case e := <-jobEvents:
			if e.Type == JobEventTypeError {
				return loggedErr(e.Error.Error())
			}
			if e.Type != JobEventTypeController {
				continue
			}
			event := e.JobEvent
			log.Info("got job event", "job_id", event.ID, "type", event.Type, "state", event.State)
			if event.State == ct.JobStateDown && event.Type == processType {
				actual++
				if actual == d.Processes[processType] {
					break loop
				}
			}
		case <-time.After(time.Duration(d.DeployTimeout) * time.Second):
			return loggedErr("timed out waiting for job events")
		}
	}

	// do a one-by-one deploy for the other process types
	return d.deployOneByOne()
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
