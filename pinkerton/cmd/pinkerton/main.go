package main

import (
	"fmt"
	"log"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/pinkerton"
)

func main() {
	usage := `Pinkerton manages Docker images.

Usage:
  pinkerton pull [options] <image-url>
  pinkerton checkout [options] <id> <image-id>
  pinkerton cleanup [options] <id>
  pinkerton -h | --help

Commands:
  pull      Download a Docker image
  checkout  Create a working copy of an image
  cleanup   Destroy a working copy of an image

Examples:
  pinkerton pull https://registry.hub.docker.com/redis
  pinkerton pull https://registry.hub.docker.com/ubuntu?tag=trusty
  pinkerton pull https://registry.hub.docker.com/flynn/slugrunner?id=1443bd6a675b959693a1a4021d660bebbdbff688d00c65ff057c46702e4b8933
  pinkerton checkout slugrunner-test 1443bd6a675b959693a1a4021d660bebbdbff688d00c65ff057c46702e4b8933
  pinkerton cleanup slugrunner-test

Options:
  -h, --help       show this message and exit
  --driver=<name>  storage driver [default: aufs]
  --root=<path>    storage root [default: /var/lib/docker]
  --json           emit json-formatted output
`

	args, _ := docopt.Parse(usage, nil, true, "", false)

	ctx, err := pinkerton.BuildContext(args.String["--driver"], args.String["--root"])
	if err != nil {
		log.Fatal(err)
	}

	switch {
	case args.Bool["pull"]:
		if err := ctx.Pull(args.String["<image-url>"], pinkerton.InfoPrinter(args.Bool["--json"]), nil); err != nil {
			log.Fatal(err)
		}
	case args.Bool["checkout"]:
		path, err := ctx.Checkout(args.String["<id>"], args.String["<image-id>"])
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(path)
	case args.Bool["cleanup"]:
		if err := ctx.Cleanup(args.String["<id>"]); err != nil {
			log.Fatal(err)
		}
	}
}
