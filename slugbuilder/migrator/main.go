package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
)

var ErrNotFound = errors.New("slug not found")

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)

	log.Printf("running slug migrator")
	if err := migrate(); err != nil {
		log.Printf("error running slug migrator: %s", err)
		os.Exit(1)
	}
}

func migrate() error {
	db := postgres.Wait(nil, nil)

	slugbuilder, err := getSlugbuilderArtifact(db)
	if err != nil {
		log.Printf("error getting slugbuilder artifact: %s", err)
		return err
	}

	artifacts, err := getActiveSlugArtifacts(db)
	if err != nil {
		log.Printf("error getting active slug artifacts: %s", err)
		return err
	}

	log.Printf("converting %d active slugs to Flynn images", len(artifacts))
	for i, artifact := range artifacts {
		log.Printf("converting slug %s (%d/%d)", artifact.ID, i+1, len(artifacts))
		newID, err := convert(slugbuilder, artifact.URI)
		if err != nil {
			if err == ErrNotFound {
				log.Printf("skipping slug %s (%d/%d): slug no longer exists", artifact.ID, i+1, len(artifacts))
				continue
			}
			log.Printf("error converting slug %s (%d/%d): %s", artifact.ID, i+1, len(artifacts), err)
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE release_artifacts SET artifact_id = $1 WHERE artifact_id = $2`, newID, artifact.ID); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Exec(`UPDATE artifacts SET deleted_at = now() WHERE artifact_id = $1`, artifact.ID); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

func getSlugbuilderArtifact(db *postgres.DB) (*ct.Artifact, error) {
	sql := `
SELECT manifest, layer_url_template FROM artifacts
WHERE meta->>'flynn.component' = 'slugbuilder'
ORDER BY created_at DESC LIMIT 1
`
	artifact := &ct.Artifact{
		Type: ct.ArtifactTypeFlynn,
	}
	var layerURLTemplate *string
	if err := db.QueryRow(sql).Scan(&artifact.RawManifest, &layerURLTemplate); err != nil {
		return nil, err
	}
	if layerURLTemplate != nil {
		artifact.LayerURLTemplate = *layerURLTemplate
	}
	return artifact, nil
}

func getActiveSlugArtifacts(db *postgres.DB) ([]*ct.Artifact, error) {
	sql := `
SELECT artifact_id, uri FROM artifacts
WHERE type = 'file'
AND deleted_at IS NULL
AND artifact_id IN (
  SELECT artifact_id FROM release_artifacts
  WHERE release_id IN (
    SELECT release_id FROM releases
    WHERE meta->>'git' = 'true'
    AND release_id IN (
      SELECT release_id
      FROM formations, json_each_text(formations.processes::json)
      WHERE processes != 'null'
      GROUP BY app_id, release_id
      HAVING SUM(value::int) > 0
    )
    OR release_id IN (
      SELECT release_id FROM apps
    )
  )
)
`
	rows, err := db.Query(sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var artifacts []*ct.Artifact
	for rows.Next() {
		var artifact ct.Artifact
		if err := rows.Scan(&artifact.ID, &artifact.URI); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, &artifact)
	}
	return artifacts, rows.Err()
}

func convert(slugbuilder *ct.Artifact, slugURL string) (string, error) {
	res, err := http.Get(slugURL)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	} else if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected HTTP status: %s", res.Status)
	}

	id := random.UUID()
	cmd := exec.Command(slugbuilder, "/bin/convert-legacy-slug.sh")
	cmd.Env = map[string]string{
		"CONTROLLER_KEY": os.Getenv("CONTROLLER_KEY"),
		"SLUG_IMAGE_ID":  id,
	}
	cmd.Stdin = res.Body
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Volumes = []*ct.VolumeReq{{Path: "/tmp", DeleteOnStop: true}}
	return id, cmd.Run()
}
