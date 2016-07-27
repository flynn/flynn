package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	ct "github.com/flynn/flynn/controller/types"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/jackc/pgx"
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
		return ct.ValidationError{Field: "type", Message: "must not be empty"}
	}
	if a.URI == "" {
		return ct.ValidationError{Field: "uri", Message: "must not be empty"}
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	if a.Type == ct.ArtifactTypeFlynn && a.Manifest == nil {
		if err := downloadManifest(a); err != nil {
			return ct.ValidationError{Field: "manifest", Message: fmt.Sprintf("failed to download from %s: %s", a.URI, err)}
		}
	}

	err = tx.QueryRow("artifact_insert", a.ID, string(a.Type), a.URI, a.Meta, a.Manifest, a.LayerURLTemplate).Scan(&a.CreatedAt)
	if postgres.IsUniquenessError(err, "") {
		tx.Rollback()
		tx, err = r.db.Begin()
		if err != nil {
			return err
		}
		var layerURLTemplate *string
		err = tx.QueryRow("artifact_select_by_type_and_uri", string(a.Type), a.URI).Scan(&a.ID, &a.Meta, &a.Manifest, &layerURLTemplate, &a.CreatedAt)
		if err != nil {
			tx.Rollback()
			return err
		}
		if layerURLTemplate != nil {
			a.LayerURLTemplate = *layerURLTemplate
		}
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := createEvent(tx.Exec, &ct.Event{
		ObjectID:   a.ID,
		ObjectType: ct.EventTypeArtifact,
	}, a); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func scanArtifact(s postgres.Scanner) (*ct.Artifact, error) {
	artifact := &ct.Artifact{}
	var typ string
	err := s.Scan(&artifact.ID, &typ, &artifact.URI, &artifact.Meta, &artifact.Manifest, &artifact.LayerURLTemplate, &artifact.CreatedAt)
	if err == pgx.ErrNoRows {
		err = ErrNotFound
	}
	artifact.Type = ct.ArtifactType(typ)
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
	var artifacts []*ct.Artifact
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (r *ArtifactRepo) ListIDs(ids ...string) (map[string]*ct.Artifact, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query("artifact_list_ids", fmt.Sprintf("{%s}", strings.Join(ids, ",")))
	if err != nil {
		return nil, err
	}
	artifacts := make(map[string]*ct.Artifact, len(ids))
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		artifacts[artifact.ID] = artifact
	}
	return artifacts, rows.Err()
}

func downloadManifest(artifact *ct.Artifact) error {
	res, err := hh.RetryClient.Get(artifact.URI)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status: %s", res.Status)
	}
	manifest := &ct.ImageManifest{}
	if err := json.NewDecoder(res.Body).Decode(manifest); err != nil {
		return err
	}
	artifact.Manifest = manifest
	return nil
}
