package main

import (
	"fmt"
	"strconv"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/go-docopt"
)

func init() {
	register("deployment", runDeployments, `
usage: flynn deployment
       flynn deployment timeout [<timeout>]

Manage app deployments

Commands:
    With no arguments, shows a list of deployments

	timeout  gets or sets the number of seconds to wait for each job to start when deploying

Examples:

	$ flynn deployment
	ID                                    STATUS    CREATED             FINISHED
	a6d470d6-9638-4d74-ae71-91c3d9887714  running   4 seconds ago
	39f8b98b-2aed-40a5-9423-ae174b3fb7a9  complete  16 seconds ago      14 seconds ago
	f415ae79-0b41-4a49-bc42-d4f90c5a36c5  failed    About a minute ago  About a minute ago
	8901a4ba-8d0a-4c84-a467-bfc095aaa75d  complete  4 minutes ago       4 minutes ago

	$ flynn deployment timeout 150

	$ flynn deployment timeout
	150
`)
}

func runDeployments(args *docopt.Args, client controller.Client) error {
	if args.Bool["timeout"] {
		if args.String["<timeout>"] != "" {
			return runSetDeployTimeout(args, client)
		}
		return runGetDeployTimeout(args, client)
	}

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

func runGetDeployTimeout(args *docopt.Args, client controller.Client) error {
	app, err := client.GetApp(mustApp())
	if err != nil {
		return err
	}
	fmt.Println(app.DeployTimeout)
	return nil
}

func runSetDeployTimeout(args *docopt.Args, client controller.Client) error {
	timeout, err := strconv.Atoi(args.String["<timeout>"])
	if err != nil {
		return fmt.Errorf("error parsing timeout %q: %s", args.String["<timeout>"], err)
	}
	return client.UpdateApp(&ct.App{
		ID:            mustApp(),
		DeployTimeout: int32(timeout),
	})
}
