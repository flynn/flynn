package main

import (
	"encoding/json"

	"github.com/flynn/go-docopt"
	"github.com/flynn/go-tuf"
)

func init() {
	register("add", cmdAdd, `
usage: tuf add [--expires=<days>] [--custom=<data>] [<path>...]

Add target file(s).

Options:
  --expires=<days>   Set the targets manifest to expire <days> days from now.
  --custom=<data>    Set custom JSON data for the target(s).
`)
}

func cmdAdd(args *docopt.Args, repo *tuf.Repo) error {
	var custom json.RawMessage
	if c := args.String["--custom"]; c != "" {
		custom = json.RawMessage(c)
	}
	paths := args.All["<path>"].([]string)
	if arg := args.String["--expires"]; arg != "" {
		expires, err := parseExpires(arg)
		if err != nil {
			return err
		}
		return repo.AddTargetsWithExpires(paths, custom, expires)
	}
	return repo.AddTargets(paths, custom)
}
