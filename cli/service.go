package main

import (
	"fmt"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
)

func init() {
	register("pause", runPause, `
usage: flynn pause <type> <service>

Pause all requests to a service. Useful for doing maintenance or database migrations.

Example:

	$ flynn pause tcp echo-service
	Backend is now paused. Waiting for backends to be drained...
	All backends drained! Run 'flynn unpause tcp echo-service' when done.
`)
	register("unpause", runUnpause, `
usage: flynn unpause <type> <service>

Unpause a backend.

Example:

	$ flynn unpause tcp echo-service
`)
}

func runPause(args *docopt.Args, client *controller.Client) error {
	drained := make(chan error)

	go func() {
		if err := client.StreamServiceDrain(args.String["<type>"], args.String["<service>"]); err != nil {
			drained <- err
			return
		}
		drained <- nil
	}()

	if err := client.PauseService(args.String["<type>"], args.String["<service>"], true); err != nil {
		return err
	}
	fmt.Println("Backend is now paused. Waiting for backends to be drained...")
	if err := <-drained; err != nil {
		return err
	}
	fmt.Printf("All backends drained! Run 'flynn unpause %s %s' when done.\n", args.String["<type>"], args.String["<service>"])
	return nil
}

func runUnpause(args *docopt.Args, client *controller.Client) error {
	err := client.PauseService(args.String["<type>"], args.String["<service>"], false)
	if err != nil {
		return err
	}
	fmt.Println("Backend is unpaused.")
	return nil
}
