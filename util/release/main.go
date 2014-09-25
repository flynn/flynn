package main

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

func main() {
	usage := `flynn-release generates Flynn releases.

Usage:
  flynn-release status <commit>
  flynn-release manifest [--output=<dest>] [--id-file=<file>] <template>
  flynn-release download [--driver=<name>] [--root=<path>] <manifest>
  flynn-release upload <manifest> [<tag>]

Options:
  -o --output=<dest>   output destination file ("-" for stdout) [default: -]
  -i --id-file=<file>  JSON file containing ID mappings
  -d --driver=<name>   image storage driver [default: aufs]
  -r --root=<path>     image storage root [default: /var/lib/docker]
`
	args, _ := docopt.Parse(usage, nil, true, "", false)

	switch {
	case args.Bool["status"]:
		status(args)
	case args.Bool["manifest"]:
		manifest(args)
	case args.Bool["download"]:
		download(args)
	case args.Bool["upload"]:
		upload(args)
	}
}
