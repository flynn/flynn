package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/bgentry/que-go"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/controller/deployer/strategies"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/shutdown"
)

type context struct {
	db     *postgres.DB
	client *controller.Client
}

func main() {
	client, err := controller.NewClient("", os.Getenv("AUTH_KEY"))
	if err != nil {
		log.Fatalln("Unable to create controller client:", err)
	}

	if err := discoverd.Register("flynn-deployer", ":"+os.Getenv("PORT")); err != nil {
		log.Fatal(err)
	}

	postgres.Wait("")
	db, err := postgres.Open("", "")
	if err != nil {
		log.Fatal(err)
	}

	cxt := context{db: db, client: client}

	pgxcfg, err := pgx.ParseURI(fmt.Sprintf("http://%s:%s@%s/%s", os.Getenv("PGUSER"), os.Getenv("PGPASSWORD"), db.Addr(), os.Getenv("PGDATABASE")))
	if err != nil {
		log.Fatal(err)
	}

	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig:   pgxcfg,
		AfterConnect: que.PrepareStatements,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer pgxpool.Close()

	q := que.NewClient(pgxpool)
	wm := que.WorkMap{"Deployment": cxt.handleJob}

	workers := que.NewWorkerPool(q, wm, 10)
	go workers.Start()
	shutdown.BeforeExit(func() { workers.Shutdown() })

	<-make(chan bool) // block and keep running
}

func (c *context) handleJob(job *que.Job) (e error) {
	var args ct.DeployID
	if err := json.Unmarshal(job.Args, &args); err != nil {
		// TODO: log error
		return err
	}
	deployment, err := c.client.GetDeployment(args.ID)
	if err != nil {
		// TODO: log error
		return err
	}
	// for recovery purposes, fetch old formation
	f, err := c.client.GetFormation(deployment.AppID, deployment.OldReleaseID)
	if err != nil {
		// TODO: log error
		return err
	}
	strategyFunc, err := strategy.Get(deployment.Strategy)
	if err != nil {
		// TODO: log error
		return err
	}
	events := make(chan ct.DeploymentEvent)
	defer close(events)
	go func() {
		for ev := range events {
			ev.DeploymentID = deployment.ID
			if err := c.sendDeploymentEvent(ev); err != nil {
				log.Print(err)
			}
		}
	}()
	defer func() {
		// rollback failed deploy
		if e != nil {
			events <- ct.DeploymentEvent{
				ReleaseID: deployment.NewReleaseID,
				Status:    "failed",
			}
			if e = c.rollback(deployment, f); e != nil {
				return
			}
		}
		e = nil
	}()
	if err := strategyFunc(c.client, deployment, events); err != nil {
		// TODO: log/handle error
		return err
	}
	if err := c.setDeploymentDone(deployment.ID); err != nil {
		log.Print(err)
	}
	if err := c.client.SetAppRelease(deployment.AppID, deployment.NewReleaseID); err != nil {
		log.Print(err)
	}
	// signal success
	events <- ct.DeploymentEvent{
		ReleaseID: deployment.NewReleaseID,
		Status:    "complete",
	}
	return nil
}

func (c *context) rollback(deployment *ct.Deployment, original *ct.Formation) error {
	if err := c.client.PutFormation(original); err != nil {
		return err
	}
	if err := c.client.DeleteFormation(deployment.AppID, deployment.NewReleaseID); err != nil {
		return err
	}
	return nil
}

func (c *context) setDeploymentDone(id string) error {
	return c.db.Exec("UPDATE deployments SET finished_at = now() WHERE deployment_id = $1", id)
}

func (c *context) sendDeploymentEvent(e ct.DeploymentEvent) error {
	if e.Status == "" {
		e.Status = "running"
	}
	query := "INSERT INTO deployment_events (deployment_id, release_id, job_type, job_state, status) VALUES ($1, $2, $3, $4, $5)"
	return c.db.Exec(query, e.DeploymentID, e.ReleaseID, e.JobType, e.JobState, e.Status)
}
