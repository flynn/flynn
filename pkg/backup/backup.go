package backup

import (
	"fmt"
	"io"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func Run(client *controller.Client, out io.Writer) error {
	tw := NewTarWriter("flynn-backup-"+time.Now().UTC().Format("2006-01-02_150405"), out)
	defer tw.Close()

	// get app and release details for key apps
	data, err := getApps(client)
	if err != nil {
		return err
	}
	if err := tw.WriteJSON("flynn.json", data); err != nil {
		return err
	}

	newJob := &ct.NewJob{
		ReleaseID:  data["postgres"].Release.ID,
		Entrypoint: []string{"sh"},
		Cmd:        []string{"-c", "pg_dumpall --clean --if-exists | gzip -9"},
		Env: map[string]string{
			"PGHOST":     "leader.postgres.discoverd",
			"PGUSER":     "flynn",
			"PGPASSWORD": data["postgres"].Release.Env["PGPASSWORD"],
		},
		DisableLog: true,
	}
	if err := tw.WriteCommandOutput(client, "postgres.sql.gz", "postgres", newJob); err != nil {
		return fmt.Errorf("error dumping database: %s", err)
	}
	return nil
}

func getApps(client *controller.Client) (map[string]*ct.ExpandedFormation, error) {
	appNames := []string{"postgres", "discoverd", "flannel", "controller"}
	data := make(map[string]*ct.ExpandedFormation, len(appNames))
	for _, name := range appNames {
		app, err := client.GetApp(name)
		if err != nil {
			return nil, fmt.Errorf("error getting %s app details: %s", name, err)
		}
		release, err := client.GetAppRelease(app.ID)
		if err != nil {
			return nil, fmt.Errorf("error getting %s app release: %s", name, err)
		}
		formation, err := client.GetFormation(app.ID, release.ID)
		if err != nil {
			return nil, fmt.Errorf("error getting %s app formation: %s", name, err)
		}
		artifact, err := client.GetArtifact(release.ArtifactID)
		if err != nil {
			return nil, fmt.Errorf("error getting %s app artifact: %s", name, err)
		}
		data[name] = &ct.ExpandedFormation{
			App:       app,
			Release:   release,
			Artifact:  artifact,
			Processes: formation.Processes,
		}
	}
	return data, nil
}
