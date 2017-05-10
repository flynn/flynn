package cli

import (
	"fmt"

	"github.com/flynn/flynn/pkg/version"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("version", runVersion, `
usage: flynn-host version [--release]

Options:
	--release   Print the release version

Show current version.
`)
}

func runVersion(args *docopt.Args) {
	if args.Bool["--release"] {
		fmt.Println(version.Release())
	} else {
		fmt.Println(version.String())
	}
}
