package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	ct "github.com/flynn/flynn/controller/types"
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
	release := &ct.Release{}
	var data []byte
	err := s.Scan(&release.ID, &release.ArtifactID, &data, &release.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
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
	data, err := json.Marshal(&releaseCopy)
	if err != nil {
		return err
	}
	if release.ID == "" {
		release.ID = random.UUID()
	}

	err = r.db.QueryRow("INSERT INTO releases (release_id, artifact_id, data) VALUES ($1, $2, $3) RETURNING created_at",
		release.ID, release.ArtifactID, data).Scan(&release.CreatedAt)
	release.ID = postgres.CleanUUID(release.ID)
	release.ArtifactID = postgres.CleanUUID(release.ArtifactID)
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

func (c *controllerAPI) SetAppRelease(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	app, err := c.getApp(params)
	if err != nil {
		respondWithError(w, err)
		return
	}

	var rid releaseID
	dec := json.NewDecoder(req.Body)
	err = dec.Decode(&rid)
	if err != nil {
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
	c.appRepo.SetRelease(app.ID, release.ID)

	// TODO: use transaction/lock
	fs, err := c.formationRepo.List(app.ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	if len(fs) == 1 && fs[0].ReleaseID != release.ID {
		if err := c.formationRepo.Add(&ct.Formation{
			AppID:     app.ID,
			ReleaseID: release.ID,
			Processes: fs[0].Processes,
		}); err != nil {
			respondWithError(w, err)
			return
		}
		if err := c.formationRepo.Remove(app.ID, fs[0].ReleaseID); err != nil {
			respondWithError(w, err)
			return
		}
	}

	httphelper.JSON(w, 200, release)
}

func (c *controllerAPI) GetAppRelease(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	app, err := c.getApp(params)
	if err != nil {
		respondWithError(w, err)
		return
	}

	release, err := c.appRepo.GetRelease(app.ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, release)
}
