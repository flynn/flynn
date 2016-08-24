package main

import (
	"fmt"
	"os"
	"os/exec"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"gopkg.in/inconshreveable/log15.v2"
)

func main() {
	log15.Info("running image migrator")
	if err := migrate(); err != nil {
		log15.Error("error running image migrator", "err", err)
		os.Exit(1)
	}
}

func migrate() error {
	db := postgres.Wait(nil, nil)

	artifacts, err := getImageArtifacts(db)
	if err != nil {
		return err
	}

	for _, artifact := range artifacts {
		newID, err := convert(artifact.URI)
		if err != nil {
			return err
		}
		if err := db.Exec(`UPDATE release_artifacts SET artifact_id = $1 WHERE artifact_id = $2`, newID, artifact.ID); err != nil {
			return err
		}
		if err := db.Exec(`UPDATE artifacts SET deleted_at = now() WHERE artifact_id = $1`, artifact.ID); err != nil {
			return err
		}
	}

	return nil
}

func getImageArtifacts(db *postgres.DB) ([]*ct.Artifact, error) {
	sql := `
SELECT artifact_id, uri FROM artifacts
WHERE type = 'docker' AND meta->>'docker-receive.repository' IS NOT NULL
AND artifact_id IN (
  SELECT artifact_id FROM release_artifacts
  WHERE release_id IN (
    SELECT release_id FROM releases
    WHERE meta->>'docker-receive' = 'true'
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
	cmd.Stderr = os.Stderr
	return id, cmd.Run()
}
