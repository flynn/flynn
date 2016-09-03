package main

import (
	"github.com/flynn/go-docopt"
)

func main() {
	usage := `flynn-release generates Flynn releases.

Usage:
  flynn-release status <commit>
  flynn-release manifest [--output=<dest>] [--image-dir=<dir>] [--image-repository=<url>] [--id-file=<file>] <template>
  flynn-release vagrant <url> <checksum> <version> <provider>
  flynn-release amis <version> <ids>
  flynn-release export <manifest> <dir>

Options:
  -o --output=<dest>           output destination file ("-" for stdout) [default: -]
  -i --id-file=<file>          JSON file containing ID mappings
  -r --image-repository=<url>  the image repository URL [default: https://dl.flynn.io/images]
  -d --image-dir=<dir>         the image manifest directory
`
	args, _ := docopt.Parse(usage, nil, true, "", false)

	switch {
	case args.Bool["status"]:
		status(args)
	case args.Bool["manifest"]:
		manifest(args)
	case args.Bool["vagrant"]:
		vagrant(args)
	case args.Bool["amis"]:
		amis(args)
	case args.Bool["export"]:
		export(args)
	}
}
