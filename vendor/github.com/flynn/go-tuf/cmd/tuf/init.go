package main

import (
	"github.com/flynn/go-docopt"
	"github.com/flynn/go-tuf"
)

func init() {
	register("init", cmdInit, `
usage: tuf init [--consistent-snapshot=false]

Initialize a new repository.

This is only required if the repository should not generate consistent
snapshots (i.e. by passing "--consistent-snapshot=false"). If consistent
snapshots should be generated, the repository will be implicitly
initialized to do so when generating keys.
  `)
}

func cmdInit(args *docopt.Args, repo *tuf.Repo) error {
	return repo.Init(args.String["--consistent-snapshot"] != "false")
}
