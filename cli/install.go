package main

import (
	"fmt"

	"github.com/flynn/go-docopt"
)

func init() {
	register("install", runInstaller, `usage: flynn install`)
}

func runInstaller(args *docopt.Args) error {
	fmt.Printf("DEPRECATED: `flynn install` has been deprecated.\nRefer to https://flynn.io/docs/installation for current installation instructions.\nAn unsupported and unmaintained snapshot of the installer binaries at the time of deprecation is available at https://dl.flynn.io/flynn-install-deprecated.tar.gz\n")
	return nil
}
