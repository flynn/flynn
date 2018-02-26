package main

import (
	"github.com/flynn/go-docopt"
	"github.com/flynn/go-tuf"
)

func init() {
	register("commit", cmdCommit, `
usage: tuf commit

Commit staged files to the repository.
`)
}

func cmdCommit(args *docopt.Args, repo *tuf.Repo) error {
	return repo.Commit()
}
