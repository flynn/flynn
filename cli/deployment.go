package main

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
)

func init() {
	register("deployment", runDeployments, `
usage: flynn deployment

Manage app deployments

Commands:
    With no arguments, shows a list of deployments

Examples:

	$ flynn deployment
	ID                                STATUS    CREATED             FINISHED
	37a63fb05fe946f18f11f741aed74d60  running   4 seconds ago
	51cbf2bba1204e94b1d847ae0122c647  complete  16 seconds ago      14 seconds ago
	12875d153f5c4c6cb64e263c4b422e8c  failed    About a minute ago  About a minute ago
	21d4a8174a4240a0b1dcb6303f40cad5  complete  4 minutes ago       4 minutes ago
`)
}

func runDeployments(args *docopt.Args, client *controller.Client) error {
	deployments, err := client.DeploymentList(mustApp())
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()

	listRec(w, "ID", "STATUS", "CREATED", "FINISHED")
	for _, d := range deployments {
		listRec(w, d.ID, d.Status, humanTime(d.CreatedAt), humanTime(d.FinishedAt))
	}
	return nil
}
