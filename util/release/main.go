package main

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

func main() {
	usage := `flynn-release generates Flynn releases.

Usage:
  flynn-release manifest [--output=<dest>] [--id-file=<file>] <template>
  flynn-release download <manifest>
  flynn-release upload <manifest> [<tag>]

Options:
  -o --output=<dest>   Output destination file ("-" for stdout) [default: -]
  -i --id-file=<file>  JSON file containing ID mappings
`
	args, _ := docopt.Parse(usage, nil, true, "", false)

	switch {
	case args.Bool["manifest"]:
		manifest(args)
	case args.Bool["download"]:
		download(args)
	case args.Bool["upload"]:
		upload(args)
	}
}
