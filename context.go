package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/dotcloud/docker/daemon/graphdriver"
	"github.com/flynn/pinkerton/registry"
	"github.com/flynn/pinkerton/store"
)

type Context struct {
	*store.Store
	driver graphdriver.Driver
	json   bool
}

func (c *Context) Pull(url string) {
	ref, err := registry.NewRef(url)
	if err != nil {
		log.Fatal(err)
	}

	if id := ref.ImageID(); id != "" && c.Exists(id) {
		c.writeLayerInfo(id, "exists")
		return
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
			c.writeLayerInfo(layer.ID, "exists")
			continue
		}

		if err := layer.Fetch(); err != nil {
			log.Fatal(err)
		}
		status := "downloaded"
		if err := c.Add(layer); err != nil {
			if err == store.ErrExists {
				status = "exists"
			} else {
				log.Fatal(err)
			}
		}
		c.writeLayerInfo(layer.ID, status)
	}

	// TODO: update sizes
}

func (c *Context) writeLayerInfo(id, status string) {
	if c.json {
		json.NewEncoder(os.Stdout).Encode(struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}{id, status})
	} else {
		fmt.Println(id, status)
	}
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
