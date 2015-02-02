package pinkerton

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/aufs"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/btrfs"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/devmapper"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/vfs"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/reexec"
	tuf "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/client"
	"github.com/flynn/flynn/pinkerton/registry"
	"github.com/flynn/flynn/pinkerton/store"
	"github.com/flynn/flynn/pkg/tufutil"
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
	Repo   string      `json:"repo"`
	ID     string      `json:"id"`
	Status LayerStatus `json:"status"`
}

type LayerStatus string

const (
	LayerStatusExists     LayerStatus = "exists"
	LayerStatusDownloaded LayerStatus = "downloaded"
)

func (c *Context) PullDocker(url string, progress chan<- LayerPullInfo) error {
	ref, err := registry.NewRef(url)
	if err != nil {
		return err
	}
	return c.pull(url, registry.NewDockerSession(ref), progress)
}

func (c *Context) PullTUF(url string, client *tuf.Client, progress chan<- LayerPullInfo) error {
	ref, err := registry.NewRef(url)
	if err != nil {
		return err
	}
	return c.pull(url, registry.NewTUFSession(client, ref), progress)
}

func (c *Context) pull(url string, session registry.Session, progress chan<- LayerPullInfo) error {
	defer func() {
		if progress != nil {
			close(progress)
		}
	}()

	sendProgress := func(id string, status LayerStatus) {
		if progress != nil {
			progress <- LayerPullInfo{Repo: session.Repo(), ID: id, Status: status}
		}
	}

	if id := session.ImageID(); id != "" && c.Exists(id) {
		sendProgress(id, LayerStatusExists)
		return nil
	}

	image, err := session.GetImage()
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
			sendProgress(layer.ID, LayerStatusExists)
			continue
		}

		status := LayerStatusDownloaded
		if err := c.Add(layer); err != nil {
			if err == store.ErrExists {
				status = LayerStatusExists
			} else {
				return err
			}
		}
		sendProgress(layer.ID, status)
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
				fmt.Println(l.Repo, l.ID, l.Status)
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

func PullImages(tufDB, repository, driver, root string, progress chan<- LayerPullInfo) error {
	local, err := tuf.FileLocalStore(tufDB)
	if err != nil {
		return err
	}
	remote, err := tuf.HTTPRemoteStore(repository, nil)
	if err != nil {
		return err
	}
	return PullImagesWithClient(tuf.NewClient(local, remote), repository, driver, root, progress)
}

func PullImagesWithClient(client *tuf.Client, repository, driver, root string, progress chan<- LayerPullInfo) error {
	tmp, err := tufutil.Download(client, "/version.json")
	if err != nil {
		return err
	}
	defer tmp.Close()

	var versions map[string]string
	if err := json.NewDecoder(tmp).Decode(&versions); err != nil {
		return err
	}

	ctx, err := BuildContext(driver, root)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(len(versions))
	for name, id := range versions {
		info := make(chan LayerPullInfo)
		go func() {
			for l := range info {
				progress <- l
			}
			wg.Done()
		}()
		url := fmt.Sprintf("%s?name=%s&id=%s", repository, name, id)
		if err := ctx.PullTUF(url, client, info); err != nil {
			return err
		}
	}
	wg.Wait()
	close(progress)
	return nil
}
