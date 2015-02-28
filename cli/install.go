package main

import (
	"fmt"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/installer"
)

func init() {
	register("install", runInstaller, fmt.Sprintf(`
usage: flynn install

Starts server for installer web interface.

Examples:

	$ flynn install
`, installer.DefaultInstanceType))
}

func runInstaller(args *docopt.Args) error {
	return installer.ServeHTTP()
}
