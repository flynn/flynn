package main

import (
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/go-docopt"
	tuf "github.com/flynn/go-tuf/client"
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
  pinkerton pull https://registry.hub.docker.com?name=redis
  pinkerton pull https://registry.hub.docker.com?name=ubuntu&tag=trusty
  pinkerton pull https://registry.hub.docker.com?name=flynn/slugrunner&id=1443bd6a675b959693a1a4021d660bebbdbff688d00c65ff057c46702e4b8933
  pinkerton checkout slugrunner-test 1443bd6a675b959693a1a4021d660bebbdbff688d00c65ff057c46702e4b8933
  pinkerton cleanup slugrunner-test

Options:
  -h, --help       show this message and exit
  --driver=<name>  storage driver [default: aufs]
  --root=<path>    storage root [default: /var/lib/docker]
  --tuf-db=<path>  pull using a go-tuf client and initialized TUF DB
  --json           emit json-formatted output
`

	args, _ := docopt.Parse(usage, nil, true, "", false)

	ctx, err := pinkerton.BuildContext(args.String["--driver"], args.String["--root"])
	if err != nil {
		log.Fatal(err)
	}

	switch {
	case args.Bool["pull"]:
		if args.String["--tuf-db"] == "" {
			if _, err := ctx.PullDocker(args.String["<image-url>"], pinkerton.DockerPullPrinter(os.Stdout)); err != nil {
				log.Fatal(err)
			}
			return
		}
		client, err := newTUFClient(args.String["<image-url>"], args.String["--tuf-db"])
		if err != nil {
			log.Fatal(err)
		}
		if err := ctx.PullTUF(args.String["<image-url>"], client, pinkerton.InfoPrinter(args.Bool["--json"])); err != nil {
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

func newTUFClient(uri, tufDB string) (*tuf.Client, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	baseURL := &url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
	remote, err := tuf.HTTPRemoteStore(baseURL.String(), nil)
	if err != nil {
		return nil, err
	}
	local, err := tuf.FileLocalStore(tufDB)
	if err != nil {
		return nil, err
	}
	client := tuf.NewClient(local, remote)
	if _, err := client.Update(); err != nil && !tuf.IsLatestSnapshot(err) {
		return nil, err
	}
	return client, nil
}
