package pinkerton

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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
	"github.com/flynn/flynn/pinkerton/layer"
	"github.com/flynn/flynn/pkg/tufutil"
	"github.com/flynn/flynn/pkg/version"
	tuf "github.com/flynn/go-tuf/client"
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

func (c *Context) PullTUF(url string, client *tuf.Client, progress chan<- layer.PullInfo) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	ref, err := NewRef(url)
	if err != nil {
		return err
	}

	defer func() {
		if progress != nil {
			close(progress)
		}
	}()
	sendProgress := func(id string, typ layer.Type, status layer.Status) {
		if progress != nil {
			progress <- layer.PullInfo{
				Repo:   ref.repo,
				Type:   typ,
				ID:     id,
				Status: status,
			}
		}
	}

	if ref.imageID != "" && c.graph.Exists(ref.imageID) {
		sendProgress(ref.imageID, layer.TypeImage, layer.StatusExists)
		return nil
	}

	session := NewTUFSession(client, ref)

	image, err := session.GetImage()
	if err != nil {
		return err
	}

	layers, err := session.GetAncestors(image.ID())
	if err != nil {
		return err
	}

	for i := len(layers) - 1; i >= 0; i-- {
		l := layers[i]
		if c.graph.Exists(l.ID()) {
			sendProgress(l.ID(), layer.TypeLayer, layer.StatusExists)
			continue
		}

		status := layer.StatusDownloaded
		if err := c.graph.Register(l, l); err != nil {
			return err
		}
		sendProgress(l.ID(), layer.TypeLayer, status)
	}

	sendProgress(image.ID(), layer.TypeImage, layer.StatusDownloaded)

	// TODO: update sizes

	return nil
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

func InfoPrinter(jsonOut bool) chan<- layer.PullInfo {
	enc := json.NewEncoder(os.Stdout)
	info := make(chan layer.PullInfo)
	go func() {
		for l := range info {
			if jsonOut {
				enc.Encode(l)
			} else {
				fmt.Println(l.Repo, l.Type, l.ID, l.Status)
			}
		}
	}()
	return info
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

func PullImages(tufDB, repository, driver, root, ver string, progress chan<- layer.PullInfo) error {
	local, err := tuf.FileLocalStore(tufDB)
	if err != nil {
		return err
	}
	opts := &tuf.HTTPRemoteOptions{
		UserAgent: fmt.Sprintf("pinkerton/%s %s-%s pull", version.String(), runtime.GOOS, runtime.GOARCH),
		Retries:   tuf.DefaultHTTPRetries,
	}
	remote, err := tuf.HTTPRemoteStore(repository, opts)
	if err != nil {
		return err
	}
	return PullImagesWithClient(tuf.NewClient(local, remote), repository, driver, root, ver, progress)
}

func PullImagesWithClient(client *tuf.Client, repository, driver, root, version string, progress chan<- layer.PullInfo) error {
	path := filepath.Join(version, "version.json.gz")
	tmp, err := tufutil.Download(client, path)
	if err != nil {
		return err
	}
	defer tmp.Close()

	gz, err := gzip.NewReader(tmp)
	if err != nil {
		return err
	}
	defer gz.Close()

	var versions map[string]string
	if err := json.NewDecoder(gz).Decode(&versions); err != nil {
		return err
	}

	ctx, err := BuildContext(driver, root)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(len(versions))
	for name, id := range versions {
		info := make(chan layer.PullInfo)
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
