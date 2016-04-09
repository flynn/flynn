package backup

import (
	"fmt"
	"io"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func Run(client *controller.Client, out io.Writer, progress ProgressBar) error {
	tw := NewTarWriter("flynn-backup-"+time.Now().UTC().Format("2006-01-02_150405"), out, progress)
	defer tw.Close()

	// get app and release details for key apps
	data, err := getApps(client)
	if err != nil {
		return err
	}
	if err := tw.WriteJSON("flynn.json", data); err != nil {
		return err
	}

	pgRelease := data["postgres"].Release
	pgJob := &ct.NewJob{
		ReleaseID:  pgRelease.ID,
		Entrypoint: []string{"bash"},
		Cmd:        []string{"-c", "set -o pipefail; pg_dumpall --clean --if-exists | gzip -9"},
		Env: map[string]string{
			"PGHOST":     pgRelease.Env["PGHOST"],
			"PGUSER":     pgRelease.Env["PGUSER"],
			"PGPASSWORD": pgRelease.Env["PGPASSWORD"],
		},
		DisableLog: true,
	}
	if err := tw.WriteCommandOutput(client, "postgres.sql.gz", "postgres", pgJob); err != nil {
		return fmt.Errorf("error dumping postgres database: %s", err)
	}

	// If mariadb is not present skip attempting to store the backup in the archive
	if mariadb, ok := data["mariadb"]; ok && mariadb.Processes["mariadb"] > 0 {
		mysqlRelease := mariadb.Release
		mysqlJob := &ct.NewJob{
			ReleaseID:  mysqlRelease.ID,
			Entrypoint: []string{"bash"},
			Cmd:        []string{"-c", fmt.Sprintf("set -o pipefail; /usr/bin/mysqldump -h %s -u %s --all-databases | gzip -9", mysqlRelease.Env["MYSQL_HOST"], mysqlRelease.Env["MYSQL_USER"])},
			Env: map[string]string{
				"MYSQL_PWD": mysqlRelease.Env["MYSQL_PWD"],
			},
			DisableLog: true,
		}
		if err := tw.WriteCommandOutput(client, "mysql.sql.gz", "mariadb", mysqlJob); err != nil {
			return fmt.Errorf("error dumping mariadb database: %s", err)
		}
	}
	return nil
}

func getApps(client *controller.Client) (map[string]*ct.ExpandedFormation, error) {
	// app -> required for backup
	apps := map[string]bool{
		"postgres":   true,
		"mariadb":    false,
		"discoverd":  true,
		"flannel":    true,
		"controller": true,
	}
	data := make(map[string]*ct.ExpandedFormation, len(apps))
	for name, required := range apps {
		app, err := client.GetApp(name)
		if err != nil {
			if required {
				return nil, fmt.Errorf("error getting %s app details: %s", name, err)
			} else {
				// If it's not an essential app just exclude it from the backup and continue.
				continue
			}
		}
		release, err := client.GetAppRelease(app.ID)
		if err != nil {
			return nil, fmt.Errorf("error getting %s app release: %s", name, err)
		}
		formation, err := client.GetFormation(app.ID, release.ID)
		if err != nil {
			return nil, fmt.Errorf("error getting %s app formation: %s", name, err)
		}
		imageArtifact, err := client.GetArtifact(release.ImageArtifactID())
		if err != nil {
			return nil, fmt.Errorf("error getting %s app artifact: %s", name, err)
		}
		fileArtifacts := make([]*ct.Artifact, len(release.FileArtifactIDs()))
		for i, artifactID := range release.FileArtifactIDs() {
			fileArtifact, err := client.GetArtifact(artifactID)
			if err != nil {
				return nil, fmt.Errorf("error getting %s app file artifact: %s", name, err)
			}
			fileArtifacts[i] = fileArtifact
		}
		data[name] = &ct.ExpandedFormation{
			App:           app,
			Release:       release,
			ImageArtifact: imageArtifact,
			FileArtifacts: fileArtifacts,
			Processes:     formation.Processes,
		}
	}
	return data, nil
}
