package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/bgentry/que-go"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/controller/deployer/strategies"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/shutdown"
)

type context struct {
	db     *postgres.DB
	client *controller.Client
}

const workerCount = 10

var logger = log15.New("app", "deployer")

func main() {
	log := logger.New("fn", "main")

	log.Info("creating controller client")
	client, err := controller.NewClient("", os.Getenv("AUTH_KEY"))
	if err != nil {
		log.Error("error creating controller client", "err", err)
		shutdown.Fatal()
	}

	log.Info("connecting to postgres")
	db := postgres.Wait("", "")

	log.Info("creating postgres connection pool")
	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     os.Getenv("PGHOST"),
			User:     os.Getenv("PGUSER"),
			Password: os.Getenv("PGPASSWORD"),
			Database: os.Getenv("PGDATABASE"),
		},
		AfterConnect:   que.PrepareStatements,
		MaxConnections: workerCount,
	})
	if err != nil {
		log.Error("error creating postgres connection pool", "err", err)
		shutdown.Fatal()
	}
	shutdown.BeforeExit(func() { pgxpool.Close() })

	ctx := context{db: db, client: client}
	workers := que.NewWorkerPool(
		que.NewClient(pgxpool),
		que.WorkMap{"deployment": ctx.HandleJob},
		workerCount,
	)
	workers.Interval = 5 * time.Second

	log.Info("starting workers", "count", workerCount, "interval", workers.Interval)
	go workers.Start()
	shutdown.BeforeExit(func() { workers.Shutdown() })

	<-make(chan bool) // block and keep running
}

func (c *context) HandleJob(job *que.Job) (e error) {
	log := logger.New("fn", "HandleJob")
	log.Info("handling job", "id", job.ID, "error_count", job.ErrorCount)

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
		log.Error("error getting old formation", "err", err)
		return err
	}

	events := make(chan ct.DeploymentEvent)
	defer close(events)
	go func() {
		log.Info("watching deployment events")
		for ev := range events {
			log.Info("received deployment event", "status", ev.Status, "type", ev.JobType, "state", ev.JobState)
			ev.DeploymentID = deployment.ID
			if err := c.createDeploymentEvent(ev); err != nil {
				log.Error("error creating deployment event record", "err", err)
			}
		}
		log.Info("stopped watching deployment events")
	}()
	defer func() {
		// rollback failed deploy
		if e != nil {
			log.Warn("rolling back deployment due to error", "err", e)
			errMsg := e.Error()
			e = c.rollback(log, deployment, f)
			events <- ct.DeploymentEvent{
				ReleaseID: deployment.NewReleaseID,
				Status:    "failed",
				Error:     errMsg,
			}
		}
	}()
	log.Info("performing deployment")
	if err := strategy.Perform(deployment, c.client, events, logger); err != nil {
		log.Error("error performing deployment", "err", err)
		return err
	}
	log.Info("setting the app release")
	if err := c.client.SetAppRelease(deployment.AppID, deployment.NewReleaseID); err != nil {
		log.Error("error setting the app release", "err", err)
		return err
	}
	log.Info("marking the deployment as done")
	if err := c.setDeploymentDone(deployment.ID); err != nil {
		log.Error("error marking the deployment as done", "err", err)
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

	log.Info("restoring the original formation")
	if err := c.client.PutFormation(original); err != nil {
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
	return c.db.Exec("UPDATE deployments SET finished_at = now() WHERE deployment_id = $1", id)
}

func (c *context) createDeploymentEvent(e ct.DeploymentEvent) error {
	if e.Status == "" {
		e.Status = "running"
	}
	query := "INSERT INTO deployment_events (deployment_id, release_id, job_type, job_state, status) VALUES ($1, $2, $3, $4, $5)"
	return c.db.Exec(query, e.DeploymentID, e.ReleaseID, e.JobType, e.JobState, e.Status)
}
