package main

import (
	"log"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
)

func runKill(argv []string, client *controller.Client) error {
	usage := `usage: flynn kill <job>

Kill a job.`
	args, _ := docopt.Parse(usage, argv, true, "", false)
	job := args.String["<job>"]

	if err := client.DeleteJob(mustApp(), job); err != nil {
		return err
	}
	log.Printf("Job %s killed.", job)
	return nil
}
