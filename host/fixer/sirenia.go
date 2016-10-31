package fixer

import (
	"encoding/json"
	"fmt"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	sirenia "github.com/flynn/flynn/pkg/sirenia/client"
	state "github.com/flynn/flynn/pkg/sirenia/state"
)

func (f *ClusterFixer) CheckSirenia(svc string) error {
	log := f.l.New("fn", "CheckSirenia", "service", svc)
	log.Info("checking sirenia cluster status")
	service := discoverd.NewService(svc)
	leader, _ := service.Leader()
	if leader == nil || leader.Addr == "" {
		log.Info("no running leader")
		leader = nil
	} else {
		log.Info("found running leader")
	}
	instances, _ := service.Instances()
	log.Info("found running instances", "count", len(instances))

	log.Info("getting sirenia status")
	var status *sirenia.Status
	if leader != nil && leader.Addr != "" {
		client := sirenia.NewClient(leader.Addr)
		var err error
		status, err = client.Status()
		if err != nil {
			log.Error("error getting status from leader", "error", err)
		}
	}
	if status != nil && status.Database != nil && status.Database.ReadWrite {
		log.Info("cluster claims to be read-write")
		return nil
	}
	return fmt.Errorf("cluster isn't read-write")
}

func (f *ClusterFixer) FixSirenia(svc string) error {
	log := f.l.New("fn", "FixSirenia", "service", svc)

	service := discoverd.NewService(svc)
	instances, _ := service.Instances()
	leader, _ := service.Leader()

	log.Info("getting service metadata")
	meta, err := service.GetMeta()
	if err != nil {
		return fmt.Errorf("error getting sirenia state from discoverd: %s", err)
	}

	var state state.State
	if err := json.Unmarshal(meta.Data, &state); err != nil {
		return fmt.Errorf("error decoding state: %s", err)
	}
	if state.Primary == nil {
		return fmt.Errorf("no primary in sirenia state")
	}

	log.Info("getting primary job info", "job.id", state.Primary.Meta["FLYNN_JOB_ID"])
	primaryJob, primaryHost, err := f.GetJob(state.Primary.Meta["FLYNN_JOB_ID"])
	if err != nil {
		log.Error("unable to get primary job info")
	}
	var syncJob *host.Job
	var syncHost *cluster.Host
	if state.Sync != nil {
		log.Info("getting sync job info", "job.id", state.Sync.Meta["FLYNN_JOB_ID"])
		syncJob, syncHost, err = f.GetJob(state.Sync.Meta["FLYNN_JOB_ID"])
		if err != nil {
			log.Error("unable to get sync job info")
		}
	}

	waitForInstance := func(jobID string) (func() (string, error), error) {
		watchCh := make(chan *discoverd.Event)
		upCh := make(chan string)
		stream, err := service.Watch(watchCh)
		if err != nil {
			return nil, fmt.Errorf("error watching discoverd service: %s", err)
		}
		go func() {
			var current bool
			for event := range watchCh {
				if event.Kind == discoverd.EventKindCurrent {
					current = true
					continue
				}
				if !current || event.Kind != discoverd.EventKindUp {
					continue
				}
				if event.Instance.Meta["FLYNN_JOB_ID"] == jobID {
					upCh <- event.Instance.Addr
				}
			}
		}()
		return func() (string, error) {
			log.Info("waiting for instance to start", "job.id", jobID)
			defer stream.Close()
			select {
			case addr := <-upCh:
				return addr, nil
			case <-time.After(time.Minute):
				return "", fmt.Errorf("timed out waiting for sirenia instance to come up")
			}
		}, nil
	}

	log.Info("terminating unassigned sirenia instances")
outer:
	for _, i := range instances {
		if i.Addr == state.Primary.Addr || (state.Sync != nil && i.Addr == state.Sync.Addr) {
			continue
		}
		for _, a := range state.Async {
			if i.Addr == a.Addr {
				continue outer
			}
		}
		// job not assigned in state, attempt to terminate it
		if jobID, ok := i.Meta["FLYNN_JOB_ID"]; ok {
			hostID, err := cluster.ExtractHostID(jobID)
			if err != nil {
				log.Error("error extracting host id from jobID", "jobID", jobID, "err", err)
			}
			h := f.Host(hostID)
			if h != nil {
				if err := h.StopJob(jobID); err != nil {
					log.Error("error stopping unassigned sirenia job", "jobID", jobID)
				}
			} else {
				log.Error("host not found", "hostID", hostID)
			}
		}
	}

	isRunning := func(addr string) bool {
		for _, i := range instances {
			if i.Addr == addr {
				return true
			}
		}
		return false
	}

	// if the leader isn't currently running then start it using primaryJob/primaryHost
	var wait func() (string, error)
	if !isRunning(state.Primary.Addr) {
		// if we don't have info about the primary job attempt to promote the sync
		if primaryJob == nil {
			if syncJob != nil {
				// set primary job to sync
				primaryJob = syncJob
				primaryHost = syncHost

				// nil out sync job now so we can re-allocate it.
				syncJob = nil
				syncHost = nil
			} else {
				return fmt.Errorf("neither primary or sync job info available")
			}
		}

		primaryJob.ID = cluster.GenerateJobID(primaryHost.ID(), "")
		f.FixJobEnv(primaryJob)
		log.Info("starting primary job", "job.id", primaryJob.ID)
		wait, err = waitForInstance(primaryJob.ID)
		if err != nil {
			return err
		}
		if err := primaryHost.AddJob(primaryJob); err != nil {
			return fmt.Errorf("error starting primary job on %s: %s", primaryHost.ID(), err)
		}
	}
	if !state.Singleton && !isRunning(state.Sync.Addr) {
		if syncHost == nil {
			for _, h := range f.hosts {
				if h.ID() != primaryHost.ID() {
					syncHost = h
					break
				}
			}
			if syncHost == nil {
				// if there are no other hosts, use the same one we put the primary on
				syncHost = primaryHost
			}
		}
		// if we don't have a sync job then copy the primary job
		// and provision a new volume
		if syncJob == nil {
			syncJob = primaryJob
			vol := &ct.VolumeReq{Path: "/data"}
			if _, err := utils.ProvisionVolume(vol, syncHost, syncJob); err != nil {
				return fmt.Errorf("error creating volume on %s: %s", syncHost.ID(), err)
			}
		}
		syncJob.ID = cluster.GenerateJobID(syncHost.ID(), "")
		f.FixJobEnv(syncJob)
		log.Info("starting sync job", "job.id", syncJob.ID)
		if wait == nil {
			wait, err = waitForInstance(syncJob.ID)
			if err != nil {
				return err
			}
		}
		if err := syncHost.AddJob(syncJob); err != nil {
			return fmt.Errorf("error starting additional job on %s: %s", syncHost.ID(), err)
		}
	}

	if wait != nil {
		addr, err := wait()
		if err != nil {
			return err
		}
		if leader != nil && leader.Addr != "" {
			addr = leader.Addr
		}
		log.Info("waiting for cluster to come up read-write", "addr", addr)
		return sirenia.NewClient(addr).WaitForReadWrite(5 * time.Minute)
	}
	return nil
}
