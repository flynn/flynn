package main

import (
	"fmt"
	"strconv"

	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/go-docopt"
)

func init() {
	register("deployment", runDeployments, `
usage: flynn deployment
       flynn deployment timeout [<timeout>]
       flynn deployment batch-size [<size>]

Manage app deployments.

Commands:
    With no arguments, shows a list of deployments

	timeout     gets or sets the number of seconds to wait for each job to start when deploying

	batch-size  gets or sets the batch size for deployments using the in-batches strategy

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

	$ flynn deployment batch-size 3

	$ flynn deployment batch-size
	3
`)
}

func runDeployments(args *docopt.Args, client controller.Client) error {
	if args.Bool["timeout"] {
		if args.String["<timeout>"] != "" {
			return runSetDeployTimeout(args, client)
		}
		return runGetDeployTimeout(args, client)
	} else if args.Bool["batch-size"] {
		if args.String["<size>"] != "" {
			return runSetDeployBatchSize(args, client)
		}
		return runGetDeployBatchSize(args, client)
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

func runGetDeployBatchSize(args *docopt.Args, client controller.Client) error {
	app, err := client.GetApp(mustApp())
	if err != nil {
		return err
	}
	batchSize := app.DeployBatchSize()
	if batchSize == nil {
		fmt.Println("not set")
	} else {
		fmt.Println(*batchSize)
	}
	return nil
}

func runSetDeployBatchSize(args *docopt.Args, client controller.Client) error {
	batchSize, err := strconv.Atoi(args.String["<size>"])
	if err != nil {
		return fmt.Errorf("error parsing batch-size %q: %s", args.String["<size>"], err)
	}
	app := &ct.App{ID: mustApp()}
	app.SetDeployBatchSize(batchSize)
	return client.UpdateApp(app)
}
