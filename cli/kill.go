package main

import (
	"log"

	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/go-docopt"
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
