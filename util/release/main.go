package main

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

func main() {
	usage := `flynn-release generates Flynn releases.

Usage:
  flynn-release status <commit>
  flynn-release manifest [--output=<dest>] [--image-url-prefix=<prefix>] [--id-file=<file>] <template>
  flynn-release upload <manifest> [<tag>]
  flynn-release vagrant <url> <checksum> <version> <provider>
  flynn-release amis <version> <ids>
  flynn-release version <version> <commit>
  flynn-release images [--output=<dest>] [--image-url-prefix=<prefix>] [--id-file=<file>] [--root=<path>] [--driver=<name>] [--checksums=<bool>]

Options:
  -o --output=<dest>              output destination file ("-" for stdout) [default: -]
  -i --id-file=<file>             JSON file containing ID mappings
  -s --image-url-prefix=<prefix>  the image URL prefix [default: https://registry.hub.docker.com/flynn]
  -d --driver=<name>              image storage driver [default: aufs]
  -r --root=<path>                image storage root [default: /var/lib/docker]
  -c --checksums=<bool>           generate image checksums [default: false]
`
	args, _ := docopt.Parse(usage, nil, true, "", false)

	switch {
	case args.Bool["status"]:
		status(args)
	case args.Bool["manifest"]:
		manifest(args)
	case args.Bool["upload"]:
		upload(args)
	case args.Bool["vagrant"]:
		vagrant(args)
	case args.Bool["amis"]:
		amis(args)
	case args.Bool["version"]:
		version(args)
	case args.Bool["images"]:
		images(args)
	}
}
