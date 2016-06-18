package main

import (
	"log"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/go-docopt"
)

func init() {
	register("provider", runProvider, `
usage: flynn provider
       flynn provider add <name> <url>

Manage resource providers associated with the controller.

Commands:
    With no arguments, displays current providers

    add  creates a new provider <name> at <url>
`)
}

func runProvider(args *docopt.Args, client controller.Client) error {
	if args.Bool["add"] {
		return runProviderAdd(args, client)
	}
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

func runProviderAdd(args *docopt.Args, client controller.Client) error {
	name := args.String["<name>"]
	url := args.String["<url>"]

	if err := client.CreateProvider(&ct.Provider{Name: name, URL: url}); err != nil {
		return err
	}

	log.Printf("Created provider %s.", name)

	return nil
}
