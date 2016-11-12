package deployment

import (
	"encoding/json"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/worker/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/que-go"
	"gopkg.in/inconshreveable/log15.v2"
)

type context struct {
	db     *postgres.DB
	client controller.Client
	logger log15.Logger
}

func JobHandler(db *postgres.DB, client controller.Client, logger log15.Logger) func(*que.Job) error {
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
		if e == worker.ErrStopped {
			return
		}
		log.Info("marking the deployment as done")
		if err := c.setDeploymentDone(deployment.ID); err != nil {
			log.Error("error marking the deployment as done", "err", err)
		}

		// rollback failed deploy
		if e != nil {
			errMsg := e.Error()
			if IsSkipRollback(e) {
				// ErrSkipRollback indicates the deploy failed in some way
				// but no further action should be taken, so set the error
				// to nil to avoid retrying the deploy
				e = nil
			} else {
				log.Warn("rolling back deployment due to error", "err", e)
				e = c.rollback(log, deployment, f, job.Stop)
			}
			events <- ct.DeploymentEvent{
				ReleaseID: deployment.NewReleaseID,
				Status:    "failed",
				Error:     errMsg,
			}
		}
	}()

	j := &DeployJob{
		Deployment:   deployment,
		client:       c.client,
		deployEvents: events,
		logger:       c.logger,
		stop:         job.Stop,
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

	log.Info("scheduling app garbage collection")
	if err := c.client.ScheduleAppGarbageCollection(deployment.AppID); err != nil {
		// just log the error, no need to rollback the deploy
		log.Error("error scheduling app garbage collection", "err", err)
	}

	return nil
}

func (c *context) rollback(l log15.Logger, deployment *ct.Deployment, original *ct.Formation, stop chan struct{}) error {
	log := l.New("fn", "rollback")

	log.Info("restoring the original formation", "release.id", original.ReleaseID)
	timeout := 10 * time.Second
	opts := ct.ScaleOptions{
		Processes: original.Processes,
		Timeout:   &timeout,
		Stop:      stop,
		JobEventCallback: func(job *ct.Job) error {
			log.Info("got job event", "job.id", job.ID, "job.type", job.Type, "job.state", job.State)
			return nil
		},
	}
	if err := c.client.ScaleAppRelease(original.AppID, original.ReleaseID, opts); err != nil {
		log.Error("error restoring the original formation", "err", err)
		return err
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
