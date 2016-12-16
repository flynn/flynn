package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/que-go"
	"github.com/jackc/pgx"
	"golang.org/x/net/context"
)

type DeploymentRepo struct {
	db *postgres.DB
	q  *que.Client
}

func NewDeploymentRepo(db *postgres.DB) *DeploymentRepo {
	q := que.NewClient(db.ConnPool)
	return &DeploymentRepo{db: db, q: q}
}

func (r *DeploymentRepo) Add(data interface{}) (*ct.Deployment, error) {
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
		return nil, err
	}
	if err := tx.QueryRow("deployment_insert", d.ID, d.AppID, oldReleaseID, d.NewReleaseID, d.Strategy, d.Processes, d.Tags, d.DeployTimeout).Scan(&d.CreatedAt); err != nil {
		tx.Rollback()
		return nil, err
	}

	// fake initial deployment
	if d.FinishedAt != nil {
		if err := tx.Exec("deployment_update_finished_at", d.ID, d.FinishedAt); err != nil {
			tx.Rollback()
			return nil, err
		}
		if err = createDeploymentEvent(tx.Exec, d, "complete"); err != nil {
			tx.Rollback()
			return nil, err
		}
		d.Status = "complete"
		return d, tx.Commit()
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	args, err := json.Marshal(ct.DeployID{ID: d.ID})
	if err != nil {
		return nil, err
	}

	tx, err = r.db.Begin()
	if err != nil {
		return nil, err
	}
	if err = createDeploymentEvent(tx.Exec, d, "pending"); err != nil {
		tx.Rollback()
		return nil, err
	}
	d.Status = "pending"

	job := &que.Job{Type: "deployment", Args: args}
	if err := r.q.EnqueueInTx(job, tx.Tx); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return d, err
}

func (r *DeploymentRepo) Get(id string) (*ct.Deployment, error) {
	row := r.db.QueryRow("deployment_select", id)
	return scanDeployment(row)
}

func (r *DeploymentRepo) List(appID string) ([]*ct.Deployment, error) {
	rows, err := r.db.Query("deployment_list", appID)
	if err != nil {
		return nil, err
	}
	var deployments []*ct.Deployment
	for rows.Next() {
		deployment, err := scanDeployment(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		deployments = append(deployments, deployment)
	}
	return deployments, rows.Err()
}

func scanDeployment(s postgres.Scanner) (*ct.Deployment, error) {
	d := &ct.Deployment{}
	var oldReleaseID *string
	var status *string
	err := s.Scan(&d.ID, &d.AppID, &oldReleaseID, &d.NewReleaseID, &d.Strategy, &status, &d.Processes, &d.Tags, &d.DeployTimeout, &d.CreatedAt, &d.FinishedAt)
	if err == pgx.ErrNoRows {
		err = ErrNotFound
	}
	if oldReleaseID != nil {
		d.OldReleaseID = *oldReleaseID
	}
	if status != nil {
		d.Status = *status
	}
	return d, err
}

func (c *controllerAPI) GetDeployment(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	deployment, err := c.deploymentRepo.Get(params.ByName("deployment_id"))
	if err != nil {
		respondWithError(w, err)
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
		AppID:         app.ID,
		NewReleaseID:  release.ID,
		Strategy:      app.Strategy,
		OldReleaseID:  oldRelease.ID,
		Processes:     oldFormation.Processes,
		Tags:          oldFormation.Tags,
		DeployTimeout: app.DeployTimeout,
	}

	if err := schema.Validate(deployment); err != nil {
		respondWithError(w, err)
		return
	}
	if procCount == 0 {
		// immediately set app release
		if err := c.appRepo.SetRelease(app, release.ID); err != nil {
			respondWithError(w, err)
			return
		}
		now := time.Now()
		deployment.FinishedAt = &now
	}

	d, err := c.deploymentRepo.Add(deployment)
	if err != nil {
		if postgres.IsUniquenessError(err, "isolate_deploys") {
			httphelper.ValidationError(w, "", "Cannot create deploy, there is already one in progress for this app.")
			return
		}
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, d)
}

func (c *controllerAPI) ListDeployments(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	app := c.getApp(ctx)
	list, err := c.deploymentRepo.List(app.ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, list)
}

func createDeploymentEvent(dbExec func(string, ...interface{}) error, d *ct.Deployment, status string) error {
	e := ct.DeploymentEvent{
		AppID:        d.AppID,
		DeploymentID: d.ID,
		ReleaseID:    d.NewReleaseID,
		Status:       status,
	}
	if err := createEvent(dbExec, &ct.Event{
		AppID:      d.AppID,
		ObjectID:   d.ID,
		ObjectType: ct.EventTypeDeployment,
	}, e); err != nil {
		return err
	}
	return nil
}
