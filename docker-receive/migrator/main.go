package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
)

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)

	log.Printf("running Docker image migrator")
	if err := migrate(); err != nil {
		log.Printf("error running Docker image migrator: %s", err)
		os.Exit(1)
	}
}

func migrate() error {
	db := postgres.Wait(nil, nil)

	artifacts, err := getActiveImageArtifacts(db)
	if err != nil {
		log.Printf("error getting active Docker image artifacts: %s", err)
		return err
	}

	log.Printf("converting %d active Docker images to Flynn images", len(artifacts))
	for i, artifact := range artifacts {
		log.Printf("converting Docker image %s (%d/%d)", artifact.ID, i+1, len(artifacts))
		newID, err := convert(artifact.URI)
		if err != nil {
			log.Printf("error converting Docker image %s (%d/%d): %s", artifact.ID, i+1, len(artifacts), err)
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

func getActiveImageArtifacts(db *postgres.DB) ([]*ct.Artifact, error) {
	sql := `
SELECT artifact_id, uri FROM artifacts
WHERE type = 'docker'
AND meta->>'docker-receive.repository' IS NOT NULL
AND deleted_at IS NULL
AND artifact_id IN (
  SELECT artifact_id FROM release_artifacts
  WHERE release_id IN (
    SELECT release_id FROM releases
    WHERE meta->>'docker-receive' = 'true'
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

func convert(imageURL string) (string, error) {
	id := random.UUID()
	cmd := exec.Command("/bin/docker-artifact", imageURL)
	cmd.Env = append(os.Environ(), fmt.Sprintf("ARTIFACT_ID=%s", id))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	return id, cmd.Run()
}
