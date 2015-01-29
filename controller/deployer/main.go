package main

import (
	"encoding/json"
	"fmt"
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
	log    log15.Logger
}

const workerCount = 10

func main() {
	log := log15.New("app", "deployer")

	client, err := controller.NewClient("", os.Getenv("AUTH_KEY"))
	if err != nil {
		log.Error("Unable to create controller client", "err", err)
		shutdown.Fatal()
	}

	postgres.Wait("")
	db, err := postgres.Open("", "")
	if err != nil {
		log.Error("Unable to connect to postgres", "err", err)
		shutdown.Fatal()
	}

	cxt := context{db: db, client: client, log: log}

	pgxcfg, err := pgx.ParseURI(fmt.Sprintf("http://%s:%s@%s/%s", os.Getenv("PGUSER"), os.Getenv("PGPASSWORD"), db.Addr(), os.Getenv("PGDATABASE")))
	if err != nil {
		log.Error("Unable to ParseURI", "err", err)
		shutdown.Fatal()
	}

	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig:     pgxcfg,
		AfterConnect:   que.PrepareStatements,
		MaxConnections: workerCount,
	})
	if err != nil {
		log.Error("Failed to create a pgx.ConnPool", "err", err)
		shutdown.Fatal()
	}
	shutdown.BeforeExit(func() { pgxpool.Close() })

	q := que.NewClient(pgxpool)
	wm := que.WorkMap{"deployment": cxt.HandleJob}

	workers := que.NewWorkerPool(q, wm, workerCount)
	workers.Interval = 5 * time.Second
	go workers.Start()
	shutdown.BeforeExit(func() { workers.Shutdown() })

	<-make(chan bool) // block and keep running
}

func (c *context) HandleJob(job *que.Job) (e error) {
	log := c.log.New("fn", "HandleJob")
	var args ct.DeployID
	if err := json.Unmarshal(job.Args, &args); err != nil {
		log.Error("Failed to extract deployment ID", "err", err)
		return err
	}
	log = c.log.New("fn", "HandleJob", "deployment_id", args.ID)
	deployment, err := c.client.GetDeployment(args.ID)
	if err != nil {
		log.Error("Failed to fetch the deployment", "at", "get_deployment", "err", err)
		return err
	}
	log = c.log.New(
		"fn", "HandleJob",
		"deployment_id", args.ID,
		"app_id", deployment.AppID,
		"old_release_id", deployment.OldReleaseID,
		"new_release_id", deployment.NewReleaseID,
		"strategy", deployment.Strategy,
	)
	// for recovery purposes, fetch old formation
	f, err := c.client.GetFormation(deployment.AppID, deployment.OldReleaseID)
	if err != nil {
		log.Error("Failed to fetch the formation", "at", "get_formation", "err", err)
		return err
	}
	strategyFunc, err := strategy.Get(deployment.Strategy)
	if err != nil {
		log.Error("Failed to determine a strategy", "at", "get_strategy", "err", err)
		return err
	}
	events := make(chan ct.DeploymentEvent)
	go func() {
		for ev := range events {
			ev.DeploymentID = deployment.ID
			if err := c.createDeploymentEvent(ev); err != nil {
				log.Error("Failed to create an event", "at", "create_deployment_event", "err", err)
			}
		}
		close(events)
	}()
	defer func() {
		// rollback failed deploy
		if e != nil {
			events <- ct.DeploymentEvent{
				ReleaseID: deployment.NewReleaseID,
				Status:    "failed",
			}
			e = c.rollback(log, deployment, f)
		}
	}()
	if err := strategyFunc(c.log, c.client, deployment, events); err != nil {
		log.Error("Error while running the strategy", "at", "run_strategy", "err", err)
		return err
	}
	if err := c.client.SetAppRelease(deployment.AppID, deployment.NewReleaseID); err != nil {
		log.Error("Error setting the app release", "at", "set_app_release", "err", err)
		return err
	}
	if err := c.setDeploymentDone(deployment.ID); err != nil {
		log.Error("Error marking the deployment as done", "at", "set_deployment_done", "err", err)
	}
	// signal success
	events <- ct.DeploymentEvent{
		ReleaseID: deployment.NewReleaseID,
		Status:    "complete",
	}
	log.Info("Deployment complete", "at", "done")
	return nil
}

func (c *context) rollback(l log15.Logger, deployment *ct.Deployment, original *ct.Formation) error {
	log := l.New("fn", "rollback")
	if err := c.client.PutFormation(original); err != nil {
		log.Error("Error restoring formation", "err", err)
		return err
	}
	if err := c.client.DeleteFormation(deployment.AppID, deployment.NewReleaseID); err != nil {
		log.Error("Failed to delete new formation:", "err", err)
		return err
	}
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
