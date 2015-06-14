package cli

import (
	"errors"
	"fmt"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	Register("stop", runStop, `
usage: flynn-host stop ID...

Stop running jobs`)
}

func runStop(args *docopt.Args, client *cluster.Client) error {
	success := true
	clients := make(map[string]*cluster.Host)
	for _, id := range args.All["ID"].([]string) {
		hostID, jobID, err := cluster.ParseJobID(id)
		if err != nil {
			fmt.Printf("could not parse %s: %s", id, err)
			success = false
			continue
		}
		hostClient, ok := clients[hostID]
		if !ok {
			var err error
			hostClient, err = client.Host(hostID)
			if err != nil {
				fmt.Printf("could not connect to host %s: %s\n", hostID, err)
				success = false
				continue
			}
			clients[hostID] = hostClient
		}
		if err := hostClient.StopJob(jobID); err != nil {
			fmt.Printf("could not stop job %s: %s\n", jobID, err)
			success = false
			continue
		}
		fmt.Println(jobID, "stopped")
	}
	if !success {
		return errors.New("could not stop all jobs")
	}
	return nil
}
