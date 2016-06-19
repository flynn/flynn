package cli

import (
	"errors"
	"fmt"

	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/go-docopt"
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
		hostID, err := cluster.ExtractHostID(id)
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
		if err := hostClient.StopJob(id); err != nil {
			fmt.Printf("could not stop job %s: %s\n", id, err)
			success = false
			continue
		}
		fmt.Println(id, "stopped")
	}
	if !success {
		return errors.New("could not stop all jobs")
	}
	return nil
}
