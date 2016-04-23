package main

import (
	"fmt"
	"net/http"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
)

type ReleaseRepo struct {
	db *postgres.DB
}

func NewReleaseRepo(db *postgres.DB) *ReleaseRepo {
	return &ReleaseRepo{db}
}

func scanRelease(s postgres.Scanner) (*ct.Release, error) {
	var artifactIDs string
	release := &ct.Release{}
	err := s.Scan(&release.ID, &artifactIDs, &release.Env, &release.Processes, &release.Meta, &release.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	if artifactIDs != "" {
		release.ArtifactIDs = split(artifactIDs[1:len(artifactIDs)-1], ",")
		release.LegacyArtifactID = release.ImageArtifactID()
	}
	return release, err
}

func (r *ReleaseRepo) Add(data interface{}) error {
	release := data.(*ct.Release)

	for typ, proc := range release.Processes {
		resource.SetDefaults(&proc.Resources)
		release.Processes[typ] = proc
	}

	if release.ID == "" {
		release.ID = random.UUID()
	}
	if release.LegacyArtifactID != "" && len(release.ArtifactIDs) == 0 {
		release.ArtifactIDs = []string{release.LegacyArtifactID}
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	err = tx.QueryRow("release_insert", release.ID, release.Env, release.Processes, release.Meta).Scan(&release.CreatedAt)
	if err != nil {
		tx.Rollback()
		return err
	}

	for _, artifactID := range release.ArtifactIDs {
		if err := tx.Exec("release_artifacts_insert", release.ID, artifactID); err != nil {
			tx.Rollback()
			if e, ok := err.(pgx.PgError); ok && e.Code == postgres.CheckViolation {
				return ct.ValidationError{
					Field:   "artifacts",
					Message: e.Message,
				}
			}
			return err
		}
	}

	if err := createEvent(tx.Exec, &ct.Event{
		ObjectID:   release.ID,
		ObjectType: ct.EventTypeRelease,
	}, release); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (r *ReleaseRepo) Get(id string) (interface{}, error) {
	row := r.db.QueryRow("release_select", id)
	return scanRelease(row)
}

func releaseList(rows *pgx.Rows) ([]*ct.Release, error) {
	var releases []*ct.Release
	for rows.Next() {
		release, err := scanRelease(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		releases = append(releases, release)
	}
	return releases, rows.Err()
}

func (r *ReleaseRepo) List() (interface{}, error) {
	rows, err := r.db.Query("release_list")
	if err != nil {
		return nil, err
	}
	return releaseList(rows)
}

func (r *ReleaseRepo) AppList(appID string) ([]*ct.Release, error) {
	rows, err := r.db.Query(`release_app_list`, appID)
	if err != nil {
		return nil, err
	}
	return releaseList(rows)
}

type releaseID struct {
	ID string `json:"id"`
}

func (c *controllerAPI) GetAppReleases(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	list, err := c.releaseRepo.AppList(c.getApp(ctx).ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, list)
}

func (c *controllerAPI) SetAppRelease(ctx context.Context, w http.ResponseWriter, req *http.Request) {
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

	if err := schema.Validate(release); err != nil {
		respondWithError(w, err)
		return
	}

	app := c.getApp(ctx)
	c.appRepo.SetRelease(app, release.ID)
	httphelper.JSON(w, 200, release)
}

func (c *controllerAPI) GetAppRelease(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	release, err := c.appRepo.GetRelease(c.getApp(ctx).ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, release)
}
