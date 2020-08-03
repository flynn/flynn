package main

import "github.com/flynn/flynn/cli/login"

func init() {
	register("login", login.Run, `
usage: flynn login [-c <controller-url> | -p] [-n <cluster-name>] [--oob-code] [-f] [<issuer>]

Authenticate. With no arguments or an existing issuer URL, re-authenticates.

Options:
	-c --controller-url=<controller-url>  the controller URL for the cluster to add
	-n --cluster-name=<cluster-name>      the name of the cluster to add (default is "default")
	-f --force                            force creation of cluster even if the name already exists
	-p --prompt                           prompt for selection of cluster
	--oob-code                            do not attempt to use a browser and local HTTP listener for OAuth
`)
}
