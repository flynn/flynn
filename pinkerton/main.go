package main

import (
	"log"

	"github.com/dotcloud/docker/daemon/graphdriver"
	_ "github.com/dotcloud/docker/daemon/graphdriver/aufs"
	_ "github.com/dotcloud/docker/daemon/graphdriver/btrfs"
	_ "github.com/dotcloud/docker/daemon/graphdriver/devmapper"
	_ "github.com/dotcloud/docker/daemon/graphdriver/vfs"
	"github.com/flynn/go-docopt"
	"github.com/flynn/pinkerton/store"
)

func init() {
	log.SetFlags(log.Lshortfile)
}

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

	root := args.String["--root"]
	driver, err := graphdriver.GetDriver(args.String["--driver"], root, nil)
	if err != nil {
		log.Fatal(err)
	}

	s, err := store.New(root, driver)
	if err != nil {
		log.Fatal(err)
	}
	ctx := &Context{Store: s, driver: driver, json: args.Bool["--json"]}

	switch {
	case args.Bool["pull"]:
		ctx.Pull(args.String["<image-url>"])
	case args.Bool["checkout"]:
		ctx.Checkout(args.String["<id>"], args.String["<image-id>"])
	case args.Bool["cleanup"]:
		ctx.Cleanup(args.String["<id>"])
	}
}
