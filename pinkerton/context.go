package pinkerton

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/aufs"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/btrfs"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/devmapper"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/vfs"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/reexec"
	"github.com/flynn/flynn/pinkerton/registry"
	"github.com/flynn/flynn/pinkerton/store"
)

func init() {
	// This will run docker-untar and docker-applyLayer in a chroot
	reexec.Init()
}

type Context struct {
	*store.Store
	driver graphdriver.Driver
}

func BuildContext(driver, root string) (*Context, error) {
	d, err := graphdriver.GetDriver(driver, root, nil)
	if err != nil {
		return nil, err
	}

	s, err := store.New(root, d)
	if err != nil {
		return nil, err
	}
	return NewContext(s, d), nil
}

func NewContext(store *store.Store, driver graphdriver.Driver) *Context {
	return &Context{Store: store, driver: driver}
}

type LayerPullInfo struct {
	ID     string      `json:"id"`
	Status LayerStatus `json:"status"`
}

type LayerStatus string

const (
	LayerStatusExists     LayerStatus = "exists"
	LayerStatusDownloaded LayerStatus = "downloaded"
)

func (c *Context) Pull(url string, progress chan<- LayerPullInfo) error {
	defer func() {
		if progress != nil {
			close(progress)
		}
	}()

	ref, err := registry.NewRef(url)
	if err != nil {
		return err
	}

	if id := ref.ImageID(); id != "" && c.Exists(id) {
		if progress != nil {
			progress <- LayerPullInfo{ID: id, Status: LayerStatusExists}
		}
		return nil
	}

	image, err := ref.Get()
	if err != nil {
		return err
	}

	layers, err := image.Ancestors()
	if err != nil {
		return err
	}

	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		if c.Exists(layer.ID) {
			if progress != nil {
				progress <- LayerPullInfo{ID: layer.ID, Status: LayerStatusExists}
			}
			continue
		}

		if err := layer.Fetch(); err != nil {
			return err
		}
		status := LayerStatusDownloaded
		if err := c.Add(layer); err != nil {
			if err == store.ErrExists {
				status = LayerStatusExists
			} else {
				return err
			}
		}
		if progress != nil {
			progress <- LayerPullInfo{ID: layer.ID, Status: status}
		}
	}

	// TODO: update sizes

	return nil
}

func (c *Context) Checkout(id, imageID string) (string, error) {
	id = "tmp-" + id
	if err := c.driver.Create(id, imageID); err != nil {
		return "", err
	}
	path, err := c.driver.Get(id, "")
	if err != nil {
		return "", err
	}
	return path, nil
}

func (c *Context) Cleanup(id string) error {
	return c.driver.Remove("tmp-" + id)
}

func InfoPrinter(jsonOut bool) chan<- LayerPullInfo {
	enc := json.NewEncoder(os.Stdout)
	info := make(chan LayerPullInfo)
	go func() {
		for l := range info {
			if jsonOut {
				enc.Encode(l)
			} else {
				fmt.Println(l.ID, l.Status)
			}
		}
	}()
	return info
}

var ErrNoImageID = errors.New("pinkerton: missing image id")

func ImageID(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	q := u.Query()
	id := q.Get("id")
	if id == "" {
		return "", ErrNoImageID
	}
	return id, nil
}
