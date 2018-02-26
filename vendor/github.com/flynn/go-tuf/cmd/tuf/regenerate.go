package main

import (
	"log"

	"github.com/flynn/go-docopt"
	"github.com/flynn/go-tuf"
)

func init() {
	register("regenerate", cmdRegenerate, `
usage: tuf regenerate [--consistent-snapshot=false]

Recreate the targets manifest.
  `)
}

func cmdRegenerate(args *docopt.Args, repo *tuf.Repo) error {
	// TODO: implement this
	log.Println("not implemented")
	return nil
}
