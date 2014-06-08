package main

import (
	"fmt"
	"log"

	"github.com/dotcloud/docker/daemon/graphdriver"
	"github.com/flynn/pinkerton/registry"
	"github.com/flynn/pinkerton/store"
)

type Context struct {
	*store.Store
	driver graphdriver.Driver
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
		fmt.Print(layer.ID)

		if err := layer.Fetch(); err != nil {
			log.Fatal(err)
		}
		if err := c.Add(layer); err != nil {
			if err == store.ErrExists {
				fmt.Print(" exists")
			} else {
				log.Fatal(err)
			}
		}
		fmt.Print("\n")
	}

	// TODO: update sizes
}

func (c *Context) Checkout(id, imageID string) {
	id = "tmp-" + id
	if err := c.driver.Create(id, imageID); err != nil {
		log.Fatal(err)
	}
	path, err := c.driver.Get(id, "")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(path)
}

func (c *Context) Cleanup(id string) {
	if err := c.driver.Remove("tmp-" + id); err != nil {
		log.Fatal(err)
	}
}
