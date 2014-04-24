package main

import (
	"time"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-controller/utils"
	"github.com/flynn/go-sql"
	"github.com/flynn/pq"
)

type ArtifactRepo struct {
	db *DB
}

func NewArtifactRepo(db *DB) *ArtifactRepo {
	return &ArtifactRepo{db}
}

func (r *ArtifactRepo) Add(data interface{}) error {
	a := data.(*ct.Artifact)
	// TODO: actually validate
	// TODO: use a transaction here
	if a.ID == "" {
		a.ID = utils.UUID()
	}
	err := r.db.QueryRow("INSERT INTO artifacts (artifact_id, type, uri) VALUES ($1, $2, $3) RETURNING created_at",
		a.ID, a.Type, a.URI).Scan(&a.CreatedAt)
	if e, ok := err.(*pq.Error); ok && e.Code.Name() == "unique_violation" {
		var deleted *time.Time
		err = r.db.QueryRow("SELECT artifact_id, created_at, deleted_at FROM artifacts WHERE type = $1 AND uri = $2",
			a.Type, a.URI).Scan(&a.ID, &a.CreatedAt, &deleted)
		if err != nil {
			return err
		}
		if deleted != nil {
			err = r.db.Exec("UPDATE artifacts SET deleted_at = NULL WHERE artifact_id = $1", a.ID)
		}
	}
	a.ID = cleanUUID(a.ID)
	return err
}

func scanArtifact(s Scanner) (*ct.Artifact, error) {
	artifact := &ct.Artifact{}
	err := s.Scan(&artifact.ID, &artifact.Type, &artifact.URI, &artifact.CreatedAt)
	if err == sql.ErrNoRows {
		err = ErrNotFound
	}
	artifact.ID = cleanUUID(artifact.ID)
	return artifact, err
}

func (r *ArtifactRepo) Get(id string) (interface{}, error) {
	row := r.db.QueryRow("SELECT artifact_id, type, uri, created_at FROM artifacts WHERE artifact_id = $1 AND deleted_at IS NULL", id)
	return scanArtifact(row)
}

func (r *ArtifactRepo) List() (interface{}, error) {
	rows, err := r.db.Query("SELECT artifact_id, type, uri, created_at FROM artifacts WHERE deleted_at IS NULL ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	artifacts := []*ct.Artifact{}
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}
