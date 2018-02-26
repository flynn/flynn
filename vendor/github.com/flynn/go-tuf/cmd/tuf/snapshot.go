package main

import (
	"github.com/flynn/go-docopt"
	"github.com/flynn/go-tuf"
)

func init() {
	register("snapshot", cmdSnapshot, `
usage: tuf snapshot [--expires=<days>] [--compression=<format>]

Update the snapshot manifest.

Options:
  --expires=<days>   Set the snapshot manifest to expire <days> days from now.
`)
}

func cmdSnapshot(args *docopt.Args, repo *tuf.Repo) error {
	// TODO: parse --compression
	if arg := args.String["--expires"]; arg != "" {
		expires, err := parseExpires(arg)
		if err != nil {
			return err
		}
		return repo.SnapshotWithExpires(tuf.CompressionTypeNone, expires)
	}
	return repo.Snapshot(tuf.CompressionTypeNone)
}
