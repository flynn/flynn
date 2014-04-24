package main

import (
	"log"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
)

var cmdResourceAdd = &Command{
	Run:   runResourceAdd,
	Usage: "resource-add <provider>",
	Short: "provision a new resource",
	Long:  "Command resource-add provisions a new resource for the app using provider.",
}

func runResourceAdd(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 1 {
		cmd.printUsage(true)
	}

	res, err := client.ProvisionResource(&ct.ResourceReq{ProviderID: args[0], Apps: []string{mustApp()}})
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
