package data

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/que-go"
	"github.com/jackc/pgx"
)

type DeploymentRepo struct {
	db            *postgres.DB
	q             *que.Client
	appRepo       *AppRepo
	releaseRepo   *ReleaseRepo
	formationRepo *FormationRepo
}

func NewDeploymentRepo(db *postgres.DB, appRepo *AppRepo, releaseRepo *ReleaseRepo, formationRepo *FormationRepo) *DeploymentRepo {
	q := que.NewClient(db.ConnPool)
	return &DeploymentRepo{db: db, q: q, appRepo: appRepo, releaseRepo: releaseRepo, formationRepo: formationRepo}
}

func (r *DeploymentRepo) Add(appID, releaseID string) (*ct.Deployment, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}

	app, err := r.appRepo.TxGet(tx, appID)
	if err != nil {
		if err == ErrNotFound {
			err = ct.ValidationError{
				Message: fmt.Sprintf("could not find app with ID %s", appID),
			}
		}
		tx.Rollback()
		return nil, err
	}

	release, err := r.releaseRepo.TxGet(tx, releaseID)
	if err != nil {
		if err == ErrNotFound {
			err = ct.ValidationError{
				Message: fmt.Sprintf("could not find release with ID %s", releaseID),
			}
		}
		tx.Rollback()
		return nil, err
	}

	oldRelease, err := r.appRepo.TxGetRelease(tx, app.ID)
	if err == ErrNotFound {
		oldRelease = nil
	} else if err != nil {
		tx.Rollback()
		return nil, err
	}

	oldFormation := &ct.Formation{}
	if oldRelease != nil {
		f, err := r.formationRepo.TxGet(tx, app.ID, oldRelease.ID)
		if err == nil {
			oldFormation = f
		} else if err != ErrNotFound {
			tx.Rollback()
			return nil, err
		}
	}
	procCount := 0
	for _, i := range oldFormation.Processes {
		procCount += i
	}

	d := &ct.Deployment{
		AppID:         app.ID,
		NewReleaseID:  release.ID,
		Strategy:      app.Strategy,
		Processes:     oldFormation.Processes,
		Tags:          oldFormation.Tags,
		DeployTimeout: app.DeployTimeout,
		BatchSize:     app.DeployBatchSize(),
	}
	if oldRelease != nil {
		d.OldReleaseID = oldRelease.ID
	}

	if err := schema.Validate(d); err != nil {
		tx.Rollback()
		return nil, err
	}
	if procCount == 0 {
		// immediately set app release
		if err := r.appRepo.TxSetRelease(tx, app, release.ID); err != nil {
			tx.Rollback()
			return nil, err
		}
		now := time.Now().Truncate(time.Microsecond) // postgres only has microsecond precision
		d.FinishedAt = &now
	}

	var oldReleaseID *string
	if oldRelease != nil {
		oldReleaseID = &oldRelease.ID
	}
	if d.ID == "" {
		d.ID = random.UUID()
	}
	if err := tx.QueryRow("deployment_insert", d.ID, d.AppID, oldReleaseID, d.NewReleaseID, d.Strategy, d.Processes, d.Tags, d.DeployTimeout, d.BatchSize).Scan(&d.CreatedAt); err != nil {
		tx.Rollback()
		if postgres.IsUniquenessError(err, "isolate_deploys") {
			return nil, ct.ValidationError{Message: "Cannot create deploy, there is already one in progress for this app."}
		}
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

	args, err := json.Marshal(ct.DeployID{ID: d.ID})
	if err != nil {
		tx.Rollback()
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
	return d, tx.Commit()
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
	err := s.Scan(&d.ID, &d.AppID, &oldReleaseID, &d.NewReleaseID, &d.Strategy, &status, &d.Processes, &d.Tags, &d.DeployTimeout, &d.BatchSize, &d.CreatedAt, &d.FinishedAt)
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

func createDeploymentEvent(dbExec func(string, ...interface{}) error, d *ct.Deployment, status string) error {
	e := ct.DeploymentEvent{
		AppID:        d.AppID,
		DeploymentID: d.ID,
		ReleaseID:    d.NewReleaseID,
		Status:       status,
	}
	return CreateEvent(dbExec, &ct.Event{
		AppID:      d.AppID,
		ObjectID:   d.ID,
		ObjectType: ct.EventTypeDeployment,
		Op:         ct.EventOpCreate,
	}, e)
}
