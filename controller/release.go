package main

import (
	"encoding/json"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	ct "github.com/flynn/flynn/controller/types"
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
