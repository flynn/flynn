package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/bgentry/que-go"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/sse"
)

type DeploymentRepo struct {
	db *postgres.DB
	q  *que.Client
}

func NewDeploymentRepo(db *postgres.DB, pgxpool *pgx.ConnPool) *DeploymentRepo {
	q := que.NewClient(pgxpool)
	return &DeploymentRepo{db: db, q: q}
}

func (r *DeploymentRepo) Add(data interface{}) error {
	d := data.(*ct.Deployment)
	if d.ID == "" {
		d.ID = random.UUID()
	}
	var oldReleaseID *string
	if d.OldReleaseID != "" {
		oldReleaseID = &d.OldReleaseID
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	query := "INSERT INTO deployments (deployment_id, app_id, old_release_id, new_release_id, strategy) VALUES ($1, $2, $3, $4, $5) RETURNING created_at"
	if err := tx.QueryRow(query, d.ID, d.AppID, oldReleaseID, d.NewReleaseID, d.Strategy).Scan(&d.CreatedAt); err != nil {
		tx.Rollback()
		return err
	}
	d.ID = postgres.CleanUUID(d.ID)
	d.OldReleaseID = postgres.CleanUUID(d.OldReleaseID)
	d.NewReleaseID = postgres.CleanUUID(d.NewReleaseID)

	// fake initial deployment
	if d.FinishedAt != nil {
		if _, err := tx.Exec("UPDATE deployments SET finished_at = $2 WHERE deployment_id = $1", d.ID, d.FinishedAt); err != nil {
			tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	args, err := json.Marshal(ct.DeployID{ID: d.ID})
	if err != nil {
		return err
	}
	// TODO: wrap all of this in a transaction once we move to pgx
	if err := r.q.Enqueue(&que.Job{
		Type: "deployment",
		Args: args,
	}); err != nil {
		return err
	}
	return nil
}

func (r *DeploymentRepo) Get(id string) (*ct.Deployment, error) {
	query := "SELECT deployment_id, app_id, old_release_id, new_release_id, strategy, created_at, finished_at FROM deployments WHERE deployment_id = $1"
	row := r.db.QueryRow(query, id)
	return scanDeployment(row)
}

func scanDeployment(s postgres.Scanner) (*ct.Deployment, error) {
	d := &ct.Deployment{}
	err := s.Scan(&d.ID, &d.AppID, &d.OldReleaseID, &d.NewReleaseID, &d.Strategy, &d.CreatedAt, &d.FinishedAt)
	if err == sql.ErrNoRows {
		err = ErrNotFound
	}
	d.ID = postgres.CleanUUID(d.ID)
	d.OldReleaseID = postgres.CleanUUID(d.OldReleaseID)
	d.NewReleaseID = postgres.CleanUUID(d.NewReleaseID)
	return d, err
}

func (c *controllerAPI) GetDeployment(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	deployment, err := c.deploymentRepo.Get(params.ByName("deployment_id"))
	if err != nil {
		respondWithError(w, err)
		return
	}
	if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		if err := streamDeploymentEvents(ctx, deployment.ID, w, c.deploymentRepo); err != nil {
			respondWithError(w, err)
		}
		return
	}
	httphelper.JSON(w, 200, deployment)
}

func (c *controllerAPI) CreateDeployment(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var rid releaseID
	if err := httphelper.DecodeJSON(req, &rid); err != nil {
		respondWithError(w, err)
		return
	}

	rel, err := c.releaseRepo.Get(rid.ID)
	if err != nil {
		if err == ErrNotFound {
			err = ct.ValidationError{
				Message: fmt.Sprintf("could not find release with ID %s", rid.ID),
			}
		}
		respondWithError(w, err)
		return
	}
	release := rel.(*ct.Release)
	app := c.getApp(ctx)

	// TODO: wrap all of this in a transaction
	oldRelease, err := c.appRepo.GetRelease(app.ID)
	if err == ErrNotFound {
		oldRelease = &ct.Release{}
	} else if err != nil {
		respondWithError(w, err)
		return
	}
	oldFormation, err := c.formationRepo.Get(app.ID, oldRelease.ID)
	if err == ErrNotFound {
		oldFormation = &ct.Formation{}
	} else if err != nil {
		respondWithError(w, err)
		return
	}
	procCount := 0
	for _, i := range oldFormation.Processes {
		procCount += i
	}

	deployment := &ct.Deployment{
		AppID:        app.ID,
		NewReleaseID: release.ID,
		Strategy:     app.Strategy,
		OldReleaseID: oldRelease.ID,
	}

	if err := schema.Validate(deployment); err != nil {
		respondWithError(w, err)
		return
	}
	if procCount == 0 {
		// immediately set app release
		if err := c.appRepo.SetRelease(app.ID, release.ID); err != nil {
			respondWithError(w, err)
			return
		}
		now := time.Now()
		deployment.FinishedAt = &now
	}

	if err := c.deploymentRepo.Add(deployment); err != nil {
		if e, ok := err.(*pq.Error); ok && e.Code.Name() == "unique_violation" && e.Constraint == "isolate_deploys" {
			httphelper.Error(w, httphelper.JSONError{
				Code:    httphelper.ValidationError,
				Message: "Cannot create deploy, there is already one in progress for this app.",
			})
			return
		}
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, deployment)
}

// Deployment events

// TODO: share with controller streamJobs
func streamDeploymentEvents(ctx context.Context, deploymentID string, w http.ResponseWriter, repo *DeploymentRepo) (err error) {
	l, _ := ctxhelper.LoggerFromContext(ctx)
	ch := make(chan *ct.DeploymentEvent)
	s := sse.NewStream(w, ch, l)
	s.Serve()

	connected := make(chan struct{})
	done := make(chan struct{})
	listenEvent := func(ev pq.ListenerEventType, listenErr error) {
		switch ev {
		case pq.ListenerEventConnected:
			close(connected)
		case pq.ListenerEventDisconnected:
			close(done)
		case pq.ListenerEventConnectionAttemptFailed:
			err = listenErr
			close(done)
		}
	}
	listener := pq.NewListener(repo.db.DSN(), 10*time.Second, time.Minute, listenEvent)
	defer listener.Close()
	listener.Listen("deployment_events:" + postgres.FormatUUID(deploymentID))

	var currID int64
	events, err := repo.listEvents(deploymentID, 0)
	if err != nil {
		return
	}
	for _, e := range events {
		currID = e.ID
		ch <- e
	}

	select {
	case <-done:
		return
	case <-connected:
	}

	for {
		select {
		case <-s.Done:
			return
		case <-done:
			return
		case n := <-listener.Notify:
			id, err := strconv.ParseInt(n.Extra, 10, 64)
			if err != nil {
				return err
			}
			if id <= currID {
				continue
			}
			e, err := repo.getEvent(id)
			if err != nil {
				return err
			}
			ch <- e
		}
	}
	return
}

func (r *DeploymentRepo) listEvents(deploymentID string, sinceID int64) ([]*ct.DeploymentEvent, error) {
	query := "SELECT event_id, deployment_id, release_id, job_type, job_state, status, created_at FROM deployment_events WHERE deployment_id = $1 AND event_id > $2"
	rows, err := r.db.Query(query, deploymentID, sinceID)
	if err != nil {
		return nil, err
	}
	var events []*ct.DeploymentEvent
	for rows.Next() {
		event, err := scanDeploymentEvent(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (r *DeploymentRepo) getEvent(id int64) (*ct.DeploymentEvent, error) {
	row := r.db.QueryRow("SELECT event_id, deployment_id, release_id, job_type, job_state, status, created_at FROM deployment_events WHERE event_id = $1", id)
	return scanDeploymentEvent(row)
}

func scanDeploymentEvent(s postgres.Scanner) (*ct.DeploymentEvent, error) {
	event := &ct.DeploymentEvent{}
	err := s.Scan(&event.ID, &event.DeploymentID, &event.ReleaseID, &event.JobType, &event.JobState, &event.Status, &event.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	event.DeploymentID = postgres.CleanUUID(event.DeploymentID)
	event.ReleaseID = postgres.CleanUUID(event.ReleaseID)
	return event, nil
}
