package main

import "github.com/flynn/flynn/cli/login"

func init() {
	register("login", login.Run, `
usage: flynn login [-c <controller-url>] [-n <cluster-name>] [--oob-code] [<issuer>]

Authenticate. With no arguments or an existing issuer URL, re-authenticates.

Options:
	-c --controller-url=<controller-url>  the controller URL for the cluster to add
	-n --cluster-name=<cluster-name>      the name of the cluster to add (will prompt if not provided)
	--oob-code                            do not attempt to use a browser and local HTTP listener for OAuth

Examples:

	$ flynn limit
	web:     cpu=1000  temp_disk=100MB  max_fd=10000  memory=1GB
	worker:  cpu=1000  temp_disk=100MB  max_fd=10000  memory=1GB
`)
}
