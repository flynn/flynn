package main

import (
	"fmt"
)

func init() {
	register("version", runVersion, `
usage: flynn version

Show flynn version string.
`)
}

func runVersion() {
	fmt.Println(Version)
}
