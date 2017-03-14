package deployment

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flynn/flynn/appliance/mariadb/mdbxlog"
	mongoxlog "github.com/flynn/flynn/appliance/mongodb/xlog"
	"github.com/flynn/flynn/appliance/postgresql/pgxlog"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/worker/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/sirenia/client"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"github.com/flynn/flynn/pkg/sirenia/xlog"
)

func (d *DeployJob) deploySirenia() (err error) {
	log := d.logger.New("fn", "deploySirenia")
	log.Info("starting sirenia deployment")

	defer func() {
		if err != nil {
			err = ErrSkipRollback{err.Error()}
		}
	}()

	loggedErr := func(format string, v ...interface{}) error {
		e := fmt.Sprintf(format, v...)
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
		return errors.New("unable to determine sirenia process type")
	}
	proc, ok := d.newRelease.Processes[processType]
	if !ok {
		return errors.New("sirenia process type not present in new release")
	}

	// if sirenia process type is scaled to 0, skip and deploy non-sirenia processes
	if d.Processes[processType] == 0 {
		log.Info("sirenia process type scale = 0, skipping")
		return d.deployOneByOne()
	}

	events := make(chan *discoverd.Event)
	stream, err := discoverd.NewService(proc.Service).Watch(events)
	if err != nil {
		log.Error("error creating service discovery watcher", "service", processType, "err", err)
		return err
	}
	defer stream.Close()

	var serviceMeta *discoverd.ServiceMeta
	timeout := time.After(d.timeout)
loop:
	for {
		select {
		case <-d.stop:
			return worker.ErrStopped
		case event, ok := <-events:
			if !ok {
				return loggedErr("service event stream closed unexpectedly: %s", stream.Err())
			}
			switch event.Kind {
			case discoverd.EventKindCurrent:
				break loop
			case discoverd.EventKindServiceMeta:
				serviceMeta = event.ServiceMeta
			case discoverd.EventKindUp:
				if event.Instance.Meta["FLYNN_RELEASE_ID"] == d.NewReleaseID {
					return loggedErr("sirenia cluster in unexpected state")
				}
			}
		case <-timeout:
			return loggedErr("timed out waiting for current service event")
		}
	}

	if serviceMeta == nil {
		return loggedErr("missing sirenia cluster state")
	}

	var state state.State
	log.Info("decoding sirenia cluster state")
	if err := json.Unmarshal(serviceMeta.Data, &state); err != nil {
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
		return loggedErr("sirenia cluster in unhealthy state (too few asyncs)")
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
		timeout := time.After(d.timeout)
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return loggedErr("service event stream closed unexpectedly: %s", stream.Err())
				}
				if event.Kind == discoverd.EventKindDown && event.Instance.ID == inst.ID {
					d.deployEvents <- ct.DeploymentEvent{
						ReleaseID: d.OldReleaseID,
						JobState:  ct.JobStateDown,
						JobType:   processType,
					}
					return nil
				}
			case <-timeout:
				return loggedErr("timed out waiting for peer to stop")
			}
		}
	}

	// newPrimary is the first new instance started, newSync the second and
	// tailAsync the tail async instance
	var newPrimary, newSync *discoverd.Instance
	tailAsync := state.Async[len(state.Async)-1]
	startInstance := func() (*discoverd.Instance, error) {
		log.Info("starting new instance")
		d.deployEvents <- ct.DeploymentEvent{
			ReleaseID: d.NewReleaseID,
			JobState:  ct.JobStateStarting,
			JobType:   processType,
		}
		d.newFormation.Processes[processType]++
		// use PutFormation rather than ScaleAppRelease so we can use a
		// custom wait loop below
		if err := d.client.PutFormation(d.newFormation); err != nil {
			log.Error("error scaling new formation up by one", "err", err)
			return nil, err
		}
		log.Info("waiting for new instance to come up")
		var inst *discoverd.Instance
		timeout := time.After(d.timeout)
	loop:
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return nil, loggedErr("service event stream closed unexpectedly: %s", stream.Err())
				}
				if event.Kind == discoverd.EventKindUp &&
					event.Instance.Meta != nil &&
					event.Instance.Meta["FLYNN_RELEASE_ID"] == d.NewReleaseID &&
					event.Instance.Meta["FLYNN_PROCESS_TYPE"] == processType {
					inst = event.Instance
					break loop
				}
			case <-timeout:
				return nil, loggedErr("timed out waiting for new instance to come up")
			}
		}
		if newPrimary == nil {
			newPrimary = inst
		} else if newSync == nil {
			newSync = inst
		}
		tailAsync = inst
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
	waitForTailXLog := func(expected xlog.Position) error {
		log.Info("waiting for tail xlog", "tail.addr", tailAsync.Addr, "xlog", expected)
		c := client.NewClient(tailAsync.Addr)
		err := c.WaitForStatus(func(status *client.Status) bool {
			if status.Database.XLog == "" {
				return false
			}
			var xlogCmp xlog.XLog
			switch processType {
			case "postgres":
				xlogCmp = pgxlog.PgXLog{}
			case "mariadb":
				xlogCmp = mdbxlog.MDBXLog{}
			case "mongodb":
				xlogCmp = mongoxlog.XLog{}
			default:
				panic(fmt.Sprintf("unknown sirenia process type: %s", processType))
			}
			cmp, err := xlogCmp.Compare(xlog.Position(status.Database.XLog), expected)
			if err != nil {
				log.Error("error comparing xlog positions", "err", err)
				return false
			}
			return cmp >= 0
		}, 3*time.Minute)
		if err != nil {
			log.Error("error waiting for tail xlog", "err", err)
			return err
		}
		return nil
	}

	// subscribe to volume events so we can wait for snapshots to be
	// created before scaling up the new release
	volEvents := make(chan *ct.Event)
	volStream, err := d.client.StreamEvents(ct.StreamEventsOptions{
		AppID:       d.AppID,
		ObjectTypes: []ct.EventType{ct.EventTypeVolume},
	}, volEvents)
	if err != nil {
		log.Error("error connecting volume event stream: %s", err)
		return err
	}

	cloneVolume := func(inst *discoverd.Instance) error {
		log := log.New("inst.addr", inst.Addr)

		log.Info("creating snapshot")
		res, err := client.NewClient(inst.Addr).CreateSnapshot()
		if err != nil {
			log.Error("error creating snapshot", "err", err)
			return err
		}

		log.Info("creating new volume", "snapshot.id", res.Snap.ID, "xlog", res.XLog)
		hostID, _ := cluster.ExtractHostID(inst.Meta["FLYNN_JOB_ID"])
		host, err := cluster.NewClient().Host(hostID)
		if err != nil {
			log.Error("error creating new volume", "err", err)
			return err
		}
		vol := &volume.Info{
			SnapshotID: res.Snap.ID,
			Meta: map[string]string{
				"flynn-controller.app":            d.AppID,
				"flynn-controller.release":        d.NewReleaseID,
				"flynn-controller.type":           processType,
				"flynn-controller.path":           "/data",
				"flynn-controller.delete_on_stop": "false",
			},
		}
		if err := host.CreateVolume("default", vol); err != nil {
			log.Error("error creating new volume", "err", err)
			return err
		}

		log.Info("waiting for volume event", "vol.id", vol.ID)
		timeout := time.After(time.Minute)
	loop:
		for {
			select {
			case event, ok := <-volEvents:
				if !ok {
					return fmt.Errorf("volume event stream closed unexpectedly: %s", volStream.Err)
				}
				var v ct.Volume
				if err := json.Unmarshal(event.Data, &v); err != nil {
					return err
				}
				if v.ID == vol.ID {
					break loop
				}
			case <-timeout:
				return errors.New("timed out waiting for volume event")
			}
		}

		// wait for the tail async to catch up with the XLog position
		// contained in the snapshot so that new instances will not be
		// ahead of the tail XLog position
		return waitForTailXLog(res.XLog)
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
		if err := cloneVolume(state.Async[i]); err != nil {
			return err
		}
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
	if err := cloneVolume(state.Sync); err != nil {
		return err
	}
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
	if err := cloneVolume(state.Primary); err != nil {
		return err
	}
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
	d.oldFormation.Processes[processType] = 0
	if err := d.scaleOldRelease(true); err != nil {
		log.Error("error scaling old formation", "err", err)
		return err
	}

	// do a one-by-one deploy for the other process types
	return d.deployOneByOne()
}
