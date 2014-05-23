package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/dotcloud/docker/daemon/graphdriver"
	_ "github.com/dotcloud/docker/daemon/graphdriver/aufs"
	_ "github.com/dotcloud/docker/daemon/graphdriver/btrfs"
	_ "github.com/dotcloud/docker/daemon/graphdriver/devmapper"
	_ "github.com/dotcloud/docker/daemon/graphdriver/vfs"
	"github.com/flynn/docker-pull/registry"
	"github.com/flynn/docker-pull/store"
)

type Context struct {
	*store.Store
}

func (c *Context) Pull(url string) {
	ref, err := registry.NewRef(url)
	if err != nil {
		log.Fatal(err)
	}

	image, err := ref.Get()
	if err != nil {
		log.Fatal(err)
	}

	layers, err := image.Ancestors()
	if err != nil {
		log.Fatal(err)
	}

	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		if c.Exists(layer.ID) {
			fmt.Println(layer.ID, "exists")
			continue
		}
		fmt.Println(layer.ID)

		// TODO: lock layer
		if err := layer.Fetch(); err != nil {
			log.Fatal(err)
		}
		if err := c.Add(layer); err != nil {
			log.Fatal(err)
		}
	}

	// TODO: update sizes
}

func init() {
	log.SetFlags(log.Lshortfile)
}

func main() {
	driverName := flag.String("driver", "aufs", "storage driver")
	root := flag.String("root", "/var/lib/docker", "storage root")
	flag.Parse()

	driver, err := graphdriver.GetDriver(*driverName, *root)
	if err != nil {
		log.Fatal(err)
	}

	s, err := store.New(*root, driver)
	if err != nil {
		log.Fatal(err)
	}
	(&Context{s}).Pull(flag.Arg(0))
}
