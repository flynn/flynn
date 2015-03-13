package main

import (
	"fmt"

	"github.com/flynn/flynn/pkg/version"
)

func init() {
	register("version", runVersion, `
usage: flynn version

Show flynn version string.
`)
}

func runVersion() {
	fmt.Println(version.String())
}
