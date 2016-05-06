package main

import (
	"log"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
)

func init() {
	register("kill", runKill, `
usage: flynn kill <job>

Kill a job.`)
}

func runKill(args *docopt.Args, client controller.Client) error {
	job := args.String["<job>"]
	if err := client.DeleteJob(mustApp(), job); err != nil {
		return err
	}
	log.Printf("Job %s killed.", job)
	return nil
}
