package main

import (
	"os"

	"github.com/flynn/flynn/installer"
	"github.com/flynn/go-docopt"
)

func init() {
	register("install", runInstaller, `
usage: flynn install

Starts server for installer web interface
and attempts to open it in the default browser.

Examples:

	$ flynn install
`)
}

func runInstaller(args *docopt.Args) error {
	return installer.ServeHTTP(os.Getenv("PORT"))
}
