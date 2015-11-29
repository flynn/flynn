package deployment

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/que-go"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/postgres"
)

var ErrStopped = errors.New("deployment stopped")

type context struct {
	db     *postgres.DB
	client *controller.Client
	logger log15.Logger
}

func JobHandler(db *postgres.DB, client *controller.Client, logger log15.Logger) func(*que.Job) error {
	return (&context{db, client, logger}).HandleDeployment
}

func (c *context) HandleDeployment(job *que.Job) (e error) {
	log := c.logger.New("fn", "HandleDeployment")
	log.Info("handling deployment", "job_id", job.ID, "error_count", job.ErrorCount)

	var args ct.DeployID
	if err := json.Unmarshal(job.Args, &args); err != nil {
		log.Error("error unmarshaling job", "err", err)
		return err
	}

	log.Info("getting deployment record", "deployment_id", args.ID)
	deployment, err := c.client.GetDeployment(args.ID)
	if err != nil {
		log.Error("error getting deployment record", "deployment_id", args.ID, "err", err)
		return err
	}

	log = log.New(
		"deployment_id", deployment.ID,
		"app_id", deployment.AppID,
		"strategy", deployment.Strategy,
	)
	// for recovery purposes, fetch old formation
	log.Info("getting old formation")
	f, err := c.client.GetFormation(deployment.AppID, deployment.OldReleaseID)
	if err != nil {
		log.Error("error getting old formation", "release_id", deployment.OldReleaseID, "err", err)
		return err
	}

	events := make(chan ct.DeploymentEvent)
	defer close(events)
	go func() {
		log.Info("watching deployment events")
		for ev := range events {
			log.Info("received deployment event", "status", ev.Status, "type", ev.JobType, "state", ev.JobState)
			ev.AppID = deployment.AppID
			ev.DeploymentID = deployment.ID
			if err := c.createDeploymentEvent(ev); err != nil {
				log.Error("error creating deployment event record", "err", err)
			}
		}
		log.Info("stopped watching deployment events")
	}()
	defer func() {
		if e == ErrStopped {
			return
		}
		log.Info("marking the deployment as done")
		if err := c.setDeploymentDone(deployment.ID); err != nil {
			log.Error("error marking the deployment as done", "err", err)
		}

		// rollback failed deploy
		if e != nil {
			errMsg := e.Error()
			if !IsSkipRollback(e) {
				log.Warn("rolling back deployment due to error", "err", e)
				e = c.rollback(log, deployment, f)
			}
			events <- ct.DeploymentEvent{
				ReleaseID: deployment.NewReleaseID,
				Status:    "failed",
				Error:     errMsg,
			}
		}
	}()

	j := &DeployJob{
		Deployment:      deployment,
		client:          c.client,
		deployEvents:    events,
		serviceNames:    make(map[string]string),
		serviceEvents:   make(chan *discoverd.Event),
		useJobEvents:    make(map[string]struct{}),
		logger:          c.logger,
		oldReleaseState: make(map[string]int, len(deployment.Processes)),
		newReleaseState: make(map[string]int, len(deployment.Processes)),
		knownJobStates:  make(map[jobIDState]struct{}),
		omni:            make(map[string]struct{}),
		stop:            job.Stop,
	}

	log.Info("performing deployment")
	if err := j.Perform(); err != nil {
		log.Error("error performing deployment", "err", err)
		return err
	}
	log.Info("setting the app release")
	if err := c.client.SetAppRelease(deployment.AppID, deployment.NewReleaseID); err != nil {
		log.Error("error setting the app release", "err", err)
		return err
	}
	// signal success
	events <- ct.DeploymentEvent{
		ReleaseID: deployment.NewReleaseID,
		Status:    "complete",
	}
	log.Info("deployment complete")
	return nil
}

func (c *context) rollback(l log15.Logger, deployment *ct.Deployment, original *ct.Formation) error {
	log := l.New("fn", "rollback")

	log.Info("creating job watcher")
	jobWatcher, err := c.client.WatchJobEvents(deployment.AppID, deployment.OldReleaseID)
	if err != nil {
		log.Error("error opening job event stream", "err", err)
		return err
	}
	appJobs, err := c.client.JobList(deployment.AppID)
	if err != nil {
		log.Error("error listing app jobs", "err", err)
		return err
	}
	runningJobs := make(map[string]int)
	for _, j := range appJobs {
		if j.ReleaseID != deployment.OldReleaseID {
			continue
		}
		if j.State == "up" {
			runningJobs[j.Type]++
		}
	}
	expectedJobEvents := make(ct.JobEvents, len(original.Processes))
	for name, count := range original.Processes {
		count = count - runningJobs[name]
		if count > 0 {
			expectedJobEvents[name] = map[string]int{"up": count}
		}
	}

	log.Info("restoring the original formation")
	if err := c.client.PutFormation(original); err != nil {
		log.Error("error restoring the original formation", "err", err)
		return err
	}

	if len(expectedJobEvents) > 0 {
		log.Info("waiting for job events")
		if err := jobWatcher.WaitFor(expectedJobEvents, 10*time.Second, nil); err != nil {
			log.Error("error waiting for job events", "err", err)
		}
	}

	log.Info("deleting the new formation")
	if err := c.client.DeleteFormation(deployment.AppID, deployment.NewReleaseID); err != nil {
		log.Error("error deleting the new formation:", "err", err)
		return err
	}

	log.Info("rollback complete")
	return nil
}

func (c *context) setDeploymentDone(id string) error {
	return c.execWithRetries("deployment_update_finished_at_now", id)
}

func (c *context) createDeploymentEvent(e ct.DeploymentEvent) error {
	if e.Status == "" {
		e.Status = "running"
	}
	return c.execWithRetries("event_insert", e.AppID, e.DeploymentID, string(ct.EventTypeDeployment), e)
}

var execAttempts = attempt.Strategy{
	Total: 10 * time.Second,
	Delay: 100 * time.Millisecond,
}

// Retry db queries in case postgres has been deployed
func (c *context) execWithRetries(query string, args ...interface{}) error {
	return execAttempts.Run(func() error {
		return c.db.Exec(query, args...)
	})
}
