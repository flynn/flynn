package main

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
)

func init() {
	register("provider", runProvider, `
usage: flynn provider

Lists resource providers that have been associated with the controller.
`)
}

func runProvider(args *docopt.Args, client *controller.Client) error {
	providers, err := client.ProviderList()
	if err != nil {
		return err
	}
	if len(providers) == 0 {
		return nil
	}

	w := tabWriter()
	defer w.Flush()

	listRec(w, "ID", "NAME", "URL")
	for _, p := range providers {
		listRec(w, p.ID, p.Name, p.URL)
	}

	return nil
}
