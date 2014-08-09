package main

import (
	"log"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func runResource(argv []string, client *controller.Client) error {
	usage := `usage: flynn resource add <provider>

Manage resources for the app.

Commands:
   add  provisions a new resource for the app using <provider>.
	`
	args, _ := docopt.Parse(usage, argv, true, "", false)

	if args.Bool["add"] {
		return runResourceAdd(args, client)
	}

	log.Fatal("Toplevel command not implemented.")
	return nil
}

func runResourceAdd(args *docopt.Args, client *controller.Client) error {
	provider := args.String["<provider>"]

	res, err := client.ProvisionResource(&ct.ResourceReq{ProviderID: provider, Apps: []string{mustApp()}})
	if err != nil {
		return err
	}

	env := make(map[string]*string)
	for k, v := range res.Env {
		s := v
		env[k] = &s
	}

	releaseID, err := setEnv(client, "", env)
	if err != nil {
		return err
	}

	log.Printf("Created resource %s and release %s.", res.ID, releaseID)

	return nil
}
