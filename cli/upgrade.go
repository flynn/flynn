package main

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	u "github.com/flynn/flynn/pkg/updater"
)

func init() {
	register("upgrade", runUpdate, `usage: flynn update

Update Flynn components.
`)
}

func runUpgrade(args *docopt.Args, client *controller.Client) error {
	updater := &u.Updater{Client: client}
	if err := updater.Update(); err != nil {
		return err
	}

	return nil
}
