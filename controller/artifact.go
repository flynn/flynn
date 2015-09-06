package main

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
)

type ArtifactRepo struct {
	db *postgres.DB
}

func NewArtifactRepo(db *postgres.DB) *ArtifactRepo {
	return &ArtifactRepo{db}
}

func (r *ArtifactRepo) Add(data interface{}) error {
	a := data.(*ct.Artifact)
	// TODO: actually validate
	if a.ID == "" {
		a.ID = random.UUID()
	}
	if a.Type == "" {
		return ct.ValidationError{"type", "must not be empty"}
	}
	if a.URI == "" {
		return ct.ValidationError{"uri", "must not be empty"}
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	err = tx.QueryRow("INSERT INTO artifacts (artifact_id, type, uri) VALUES ($1, $2, $3) RETURNING created_at",
		a.ID, a.Type, a.URI).Scan(&a.CreatedAt)
	if postgres.IsUniquenessError(err, "") {
		tx.Rollback()
		tx, err = r.db.Begin()
		if err != nil {
			return err
		}
		err = tx.QueryRow("SELECT artifact_id, created_at FROM artifacts WHERE type = $1 AND uri = $2",
			a.Type, a.URI).Scan(&a.ID, &a.CreatedAt)
		if err != nil {
			tx.Rollback()
			return err
		}
	} else if err == nil {
		if err := createEvent(tx.Exec, &ct.Event{
			ObjectID:   a.ID,
			ObjectType: ct.EventTypeArtifact,
		}, a); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func scanArtifact(s postgres.Scanner) (*ct.Artifact, error) {
	artifact := &ct.Artifact{}
	err := s.Scan(&artifact.ID, &artifact.Type, &artifact.URI, &artifact.CreatedAt)
	if err == pgx.ErrNoRows {
		err = ErrNotFound
	}
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
