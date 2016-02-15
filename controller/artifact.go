package main

import (
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
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

	err = tx.QueryRow("artifact_insert", a.ID, string(a.Type), a.URI, a.Attributes).Scan(&a.CreatedAt)
	if postgres.IsUniquenessError(err, "") {
		tx.Rollback()
		tx, err = r.db.Begin()
		if err != nil {
			return err
		}
		err = tx.QueryRow("artifact_select_by_type_and_uri", string(a.Type), a.URI).Scan(&a.ID, &a.Attributes, &a.CreatedAt)
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
	var typ string
	err := s.Scan(&artifact.ID, &typ, &artifact.URI, &artifact.Attributes, &artifact.CreatedAt)
	if err == pgx.ErrNoRows {
		err = ErrNotFound
	}
	artifact.Type = host.ArtifactType(typ)
	return artifact, err
}

func (r *ArtifactRepo) Get(id string) (interface{}, error) {
	row := r.db.QueryRow("artifact_select", id)
	return scanArtifact(row)
}

func (r *ArtifactRepo) List() (interface{}, error) {
	rows, err := r.db.Query("artifact_list")
	if err != nil {
		return nil, err
	}
	return scanArtifacts(rows)
}

func (r *ArtifactRepo) ListIDs(ids ...string) (interface{}, error) {
	rows, err := r.db.Query("artifact_list_ids", strings.Join(ids, ","))
	if err != nil {
		return nil, err
	}
	return scanArtifacts(rows)
}

func scanArtifacts(rows *pgx.Rows) (interface{}, error) {
	var artifacts []*ct.Artifact
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
