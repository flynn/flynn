package main

import (
	"github.com/flynn/flynn-controller/client"
)

var cmdProviders = &Command{
	Run:   runProviders,
	Usage: "providers",
	Short: "list resource providers",
	Long:  "Command providers lists resource providers that have been associated with the controller",
}

func runProviders(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 0 {
		cmd.printUsage(true)
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
