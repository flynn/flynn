package pinkerton

import (
	"errors"
	"io"
	"path/filepath"
	"sync"

	"github.com/docker/docker/daemon/graphdriver"
	_ "github.com/docker/docker/daemon/graphdriver/aufs"
	"github.com/docker/docker/distribution"
	"github.com/docker/docker/distribution/metadata"
	"github.com/docker/docker/distribution/xfer"
	"github.com/docker/docker/image"
	"github.com/docker/docker/layer"
	"github.com/docker/docker/pkg/progress"
	"github.com/docker/docker/pkg/reexec"
	"github.com/docker/docker/reference"
	"github.com/docker/docker/registry"
	"github.com/docker/engine-api/types"
	"golang.org/x/net/context"
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
	driver          graphdriver.Driver
	registryService registry.Service
	metadataStore   metadata.Store
	imageStore      image.Store
	layerStore      layer.Store
	referenceStore  reference.Store
	downloadManager *xfer.LayerDownloadManager
	mtx             sync.Mutex
}

func BuildContext(driver, root string) (*Context, error) {
	d, err := graphdriver.GetDriver(driver, root, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	registryService := registry.NewService(registry.ServiceOptions{
		InsecureRegistries: internalDockerEndpoints,
		V2Only:             true,
	})

	imageRoot := filepath.Join(root, "image", driver)

	metadataStore, err := metadata.NewFSMetadataStore(filepath.Join(imageRoot, "distribution"))
	if err != nil {
		return nil, err
	}

	layerStore, err := layer.NewStoreFromOptions(layer.StoreOptions{
		StorePath:                 root,
		MetadataStorePathTemplate: filepath.Join(root, "image", "%s", "layerdb"),
		GraphDriver:               driver,
	})
	if err != nil {
		return nil, err
	}

	ifs, err := image.NewFSStoreBackend(filepath.Join(imageRoot, "imagedb"))
	if err != nil {
		return nil, err
	}

	imageStore, err := image.NewImageStore(ifs, layerStore)
	if err != nil {
		return nil, err
	}

	referenceStore, err := reference.NewReferenceStore(filepath.Join(imageRoot, "repositories.json"))
	if err != nil {
		return nil, err
	}

	return &Context{
		driver:          d,
		registryService: registryService,
		metadataStore:   metadataStore,
		imageStore:      imageStore,
		layerStore:      layerStore,
		referenceStore:  referenceStore,
		downloadManager: xfer.NewLayerDownloadManager(layerStore, 1),
	}, nil
}

func (c *Context) PullDocker(url string) (*image.Image, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	ref, err := NewRef(url)
	if err != nil {
		return nil, err
	}

	dockerRef, err := reference.ParseNamed(ref.DockerRef())
	if err != nil {
		return nil, err
	}

	// if the URL contains a digest, check if it already exists
	if ref, isCanonical := dockerRef.(reference.Canonical); isCanonical {
		if img, err := c.imageStore.Get(image.ID(ref.Digest())); err == nil {
			return img, nil
		}
	}

	pullConfig := &distribution.ImagePullConfig{
		AuthConfig: &types.AuthConfig{
			Username: ref.username,
			Password: ref.password,
		},
		ProgressOutput:   &nopProgress{},
		RegistryService:  c.registryService,
		ImageEventLogger: func(id, name, action string) {},
		MetadataStore:    c.metadataStore,
		ImageStore:       c.imageStore,
		ReferenceStore:   c.referenceStore,
		DownloadManager:  c.downloadManager,
	}
	if err := distribution.Pull(context.Background(), dockerRef, pullConfig); err != nil {
		return nil, err
	}

	id, err := c.referenceStore.Get(dockerRef)
	if err != nil {
		return nil, err
	}
	return c.imageStore.Get(id)
}

type nopProgress struct{}

func (nopProgress) WriteProgress(progress.Progress) error {
	return nil
}

type DockerLayer struct {
	ID      string
	Tags    []string
	ChainID layer.ChainID

	ctx *Context
}

func (c *Context) History(img *image.Image) ([]*DockerLayer, error) {
	history := make([]*DockerLayer, len(img.History))
	layerCounter := 0
	rootFS := *img.RootFS
	rootFS.DiffIDs = nil

	for i, h := range img.History {
		l := &DockerLayer{ID: "<missing>", ctx: c}
		if !h.EmptyLayer {
			if len(img.RootFS.DiffIDs) <= layerCounter {
				return nil, errors.New("too many non-empty layers in History section")
			}
			rootFS.Append(img.RootFS.DiffIDs[layerCounter])
			l.ChainID = rootFS.ChainID()
			layerCounter++
		}
		history[i] = l
	}

	// Fill in image IDs and tags
	histImg := img
	id := img.ID()
	for i := len(history) - 1; i >= 0; i-- {
		l := history[i]
		l.ID = id.String()

		for _, r := range c.referenceStore.References(id) {
			if _, ok := r.(reference.NamedTagged); ok {
				l.Tags = append(l.Tags, r.String())
			}
		}

		id = histImg.Parent
		if id == "" {
			break
		}
		var err error
		histImg, err = c.imageStore.Get(id)
		if err != nil {
			break
		}
	}

	return history, nil
}

type layerDiff struct {
	diff  io.ReadCloser
	ctx   *Context
	layer layer.Layer
}

func (l *layerDiff) Read(p []byte) (int, error) {
	return l.diff.Read(p)
}

func (l *layerDiff) Close() error {
	if err := l.diff.Close(); err != nil {
		return err
	}
	_, err := l.ctx.layerStore.Release(l.layer)
	return err
}

func (c *Context) Diff(chainID layer.ChainID) (io.ReadCloser, error) {
	layer, err := c.layerStore.Get(chainID)
	if err != nil {
		return nil, err
	}
	diff, err := layer.TarStream()
	if err != nil {
		return nil, err
	}
	return &layerDiff{diff, c, layer}, nil
}

func (c *Context) LookupImage(name string) (*image.Image, error) {
	ref, err := reference.ParseNamed(name)
	if err != nil {
		return nil, err
	}
	id, err := c.referenceStore.Get(ref)
	if err != nil {
		return nil, err
	}
	return c.imageStore.Get(id)
}
