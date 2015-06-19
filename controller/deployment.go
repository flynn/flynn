package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq/hstore"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/que-go"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
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
	procs := procsHstore(d.Processes)
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	query := "INSERT INTO deployments (deployment_id, app_id, old_release_id, new_release_id, strategy, processes) VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at"
	if err := tx.QueryRow(query, d.ID, d.AppID, oldReleaseID, d.NewReleaseID, d.Strategy, procs).Scan(&d.CreatedAt); err != nil {
		tx.Rollback()
		return err
	}
	d.ID = postgres.CleanUUID(d.ID)
	d.AppID = postgres.CleanUUID(d.AppID)
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
	query := "SELECT deployment_id, app_id, old_release_id, new_release_id, strategy, processes, created_at, finished_at FROM deployments WHERE deployment_id = $1"
	row := r.db.QueryRow(query, id)
	return scanDeployment(row)
}

func scanDeployment(s postgres.Scanner) (*ct.Deployment, error) {
	d := &ct.Deployment{}
	var procs hstore.Hstore
	err := s.Scan(&d.ID, &d.AppID, &d.OldReleaseID, &d.NewReleaseID, &d.Strategy, &procs, &d.CreatedAt, &d.FinishedAt)
	if err == sql.ErrNoRows {
		err = ErrNotFound
	}
	d.Processes = make(map[string]int, len(procs.Map))
	for k, v := range procs.Map {
		n, _ := strconv.Atoi(v.String)
		if n > 0 {
			d.Processes[k] = n
		}
	}
	d.ID = postgres.CleanUUID(d.ID)
	d.AppID = postgres.CleanUUID(d.AppID)
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
		Processes:    oldFormation.Processes,
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

	if err := c.deploymentRepo.Add(deployment); err != nil {
		if postgres.IsUniquenessError(err, "isolate_deploys") {
			httphelper.ValidationError(w, "", "Cannot create deploy, there is already one in progress for this app.")
			return
		}
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, deployment)
}
