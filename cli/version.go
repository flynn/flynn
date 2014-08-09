package main

import (
	"fmt"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

// NoClient
func runVersion(argv []string) error {
	usage := `usage: flynn version

Show flynn version string.
	`
	docopt.Parse(usage, argv, true, "", false)

	fmt.Println(Version)
	return nil
}
