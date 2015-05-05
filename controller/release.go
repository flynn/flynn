package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
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
	var artifactID *string
	release := &ct.Release{}
	var data []byte
	err := s.Scan(&release.ID, &artifactID, &data, &release.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	if artifactID != nil {
		release.ArtifactID = *artifactID
	}
	release.ID = postgres.CleanUUID(release.ID)
	release.ArtifactID = postgres.CleanUUID(release.ArtifactID)
	err = json.Unmarshal(data, release)
	return release, err
}

func (r *ReleaseRepo) Add(data interface{}) error {
	release := data.(*ct.Release)
	releaseCopy := *release

	releaseCopy.ID = ""
	releaseCopy.ArtifactID = ""
	releaseCopy.CreatedAt = nil

	for typ, proc := range releaseCopy.Processes {
		resource.SetDefaults(&proc.Resources)
		releaseCopy.Processes[typ] = proc
	}

	data, err := json.Marshal(&releaseCopy)
	if err != nil {
		return err
	}
	if release.ID == "" {
		release.ID = random.UUID()
	}

	var artifactID *string
	if release.ArtifactID != "" {
		artifactID = &release.ArtifactID
	}

	err = r.db.QueryRow("INSERT INTO releases (release_id, artifact_id, data) VALUES ($1, $2, $3) RETURNING created_at",
		release.ID, artifactID, data).Scan(&release.CreatedAt)

	release.ID = postgres.CleanUUID(release.ID)
	if release.ArtifactID != "" {
		release.ArtifactID = postgres.CleanUUID(release.ArtifactID)
	}
	return err
}

func (r *ReleaseRepo) Get(id string) (interface{}, error) {
	row := r.db.QueryRow("SELECT release_id, artifact_id, data, created_at FROM releases WHERE release_id = $1 AND deleted_at IS NULL", id)
	return scanRelease(row)
}

func (r *ReleaseRepo) List() (interface{}, error) {
	rows, err := r.db.Query("SELECT release_id, artifact_id, data, created_at FROM releases WHERE deleted_at IS NULL ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	releases := []*ct.Release{}
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

type releaseID struct {
	ID string `json:"id"`
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
	c.appRepo.SetRelease(app.ID, release.ID)
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
