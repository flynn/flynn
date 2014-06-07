package main

import (
	"log"

	"github.com/docopt/docopt-go"
)

func init() {
	log.SetFlags(log.Lshortfile)
}

func main() {
	usage := `Pinkerton manages Docker images.

Usage:
  pinkerton pull [options] <image-url>
  pinkerton -h | --help

Options:
  -h, --help       show this message and exit
  --driver=<name>  storage driver [default: aufs]
  --root=<path>    storage root [default: /var/lib/docker]
`

	args, _ := docopt.Parse(usage, nil, true, "", false)

	switch {
	case args["pull"].(bool):
		pull(args["--driver"].(string), args["--root"].(string), args["<image-url>"].(string))
	}
}
