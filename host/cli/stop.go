package cli

import (
	"errors"
	"fmt"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/pkg/cluster"
)

func init() {
	Register("stop", runStop, "usage: flynn-host stop ID...")
}

func runStop(args *docopt.Args, client cluster.Host) error {
	success := true
	for _, id := range args.All["ID"].([]string) {
		if err := client.StopJob(id); err != nil {
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
