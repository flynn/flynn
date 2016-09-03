package pinkerton

import (
	"errors"
	"io"
	"net/url"
	"path/filepath"
	"sync"

	docker "github.com/docker/docker/api/types"
	"github.com/docker/docker/cliconfig"
	"github.com/docker/docker/daemon/events"
	"github.com/docker/docker/daemon/graphdriver"
	_ "github.com/docker/docker/daemon/graphdriver/aufs"
	"github.com/docker/docker/graph"
	"github.com/docker/docker/image"
	"github.com/docker/docker/opts"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/reexec"
	"github.com/docker/docker/pkg/term"
	"github.com/docker/docker/registry"
)

var internalDockerEndpoints = []string{
	"docker-receive.discoverd",
	"100.100.0.0/16",
}

func init() {
	// This will run docker-untar and docker-applyLayer in a chroot
	reexec.Init()
}

type Context struct {
	store  *graph.TagStore
	graph  *graph.Graph
	driver graphdriver.Driver
	mtx    sync.Mutex
}

func BuildContext(driver, root string) (*Context, error) {
	d, err := graphdriver.GetDriver(driver, root, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	g, err := graph.NewGraph(filepath.Join(root, "graph"), d, nil, nil)
	if err != nil {
		return nil, err
	}

	config := &graph.TagStoreConfig{
		Graph:  g,
		Events: events.New(),
		Registry: registry.NewService(&registry.Options{
			Mirrors:            opts.NewListOpts(nil),
			InsecureRegistries: *opts.NewListOptsRef(&internalDockerEndpoints, nil),
		}),
	}
	store, err := graph.NewTagStore(filepath.Join(root, "repositories-"+d.String()), config)
	if err != nil {
		return nil, err
	}

	return NewContext(store, g, d), nil
}

func NewContext(store *graph.TagStore, graph *graph.Graph, driver graphdriver.Driver) *Context {
	return &Context{store: store, graph: graph, driver: driver}
}

func (c *Context) PullDocker(url string, out io.Writer) (string, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	ref, err := NewRef(url)
	if err != nil {
		return "", err
	}

	if ref.imageID != "" && c.graph.Exists(ref.imageID) {
		return ref.imageID, nil
	}

	if img, err := c.store.LookupImage(ref.DockerRef()); err == nil && img != nil {
		return img.ID, nil
	}

	config := &graph.ImagePullConfig{
		AuthConfig: &cliconfig.AuthConfig{
			Username: ref.username,
			Password: ref.password,
		},
		OutStream: out,
	}
	if err := c.store.Pull(ref.DockerRepo(), ref.Tag(), config); err != nil {
		return "", err
	}

	img, err := c.store.LookupImage(ref.DockerRef())
	if err != nil {
		return "", err
	}

	return img.ID, nil
}

func (c *Context) Checkout(id, imageID string) (string, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

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
	c.mtx.Lock()
	defer c.mtx.Unlock()

	id = "tmp-" + id
	if err := c.driver.Put(id); err != nil {
		return err
	}
	return c.driver.Remove(id)
}

func (c *Context) History(name string) ([]*docker.ImageHistory, error) {
	return c.store.History(name)
}

func (c *Context) Diff(id, parent string) (archive.Archive, error) {
	return c.driver.Diff(id, parent)
}

func (c *Context) LookupImage(name string) (*image.Image, error) {
	return c.store.LookupImage(name)
}

func DockerPullPrinter(out io.Writer) io.Writer {
	rd, wr := io.Pipe()
	termOut, isTerm := term.GetFdInfo(out)
	go jsonmessage.DisplayJSONMessagesStream(rd, out, termOut, isTerm)
	return wr
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
