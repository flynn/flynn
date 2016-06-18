package cli

import (
	"fmt"
	"strconv"

	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("signal", runSignal, `
usage: flynn-host signal ID SIGNAL

Signal a job`)
}

func runSignal(args *docopt.Args, client *cluster.Client) error {
	id := args.String["ID"]
	sig, err := strconv.Atoi(args.String["SIGNAL"])
	if err != nil {
		fmt.Println("invalid value for SIGNAL")
		return err
	}
	hostID, err := cluster.ExtractHostID(id)
	if err != nil {
		fmt.Println("could not parse", id)
		return err
	}
	hostClient, err := client.Host(hostID)
	if err != nil {
		fmt.Println("could not connect to host", hostID)
		return err
	}
	if err := hostClient.SignalJob(id, sig); err != nil {
		fmt.Println("could not signal job", id)
		return err
	}
	fmt.Printf("sent signal %d to %s successfully\n", sig, id)
	return nil
}
