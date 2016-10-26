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

	"github.com/docker/docker/daemon/graphdriver"
	_ "github.com/docker/docker/daemon/graphdriver/aufs"
	"github.com/docker/docker/distribution"
	"github.com/docker/docker/distribution/metadata"
	"github.com/docker/docker/distribution/xfer"
	"github.com/docker/docker/image"
	"github.com/docker/docker/image/v1"
	dlayer "github.com/docker/docker/layer"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/progress"
	"github.com/docker/docker/pkg/reexec"
	"github.com/docker/docker/pkg/streamformatter"
	"github.com/docker/docker/pkg/term"
	"github.com/docker/docker/reference"
	"github.com/docker/docker/registry"
	"github.com/docker/engine-api/types"
	"github.com/flynn/flynn/pinkerton/layer"
	"github.com/flynn/flynn/pkg/tufutil"
	"github.com/flynn/flynn/pkg/version"
	tuf "github.com/flynn/go-tuf/client"
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
	layerStore      dlayer.Store
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

	layerStore, err := dlayer.NewStoreFromOptions(dlayer.StoreOptions{
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

func (c *Context) PullDocker(url string, progress progress.Output) (*image.Image, error) {
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
		ProgressOutput:   progress,
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

	dockerRef, err := reference.ParseNamed(ref.DockerRef())
	if err != nil {
		return err
	}

	if id, err := c.referenceStore.Get(dockerRef); err == nil {
		sendProgress(id.String(), layer.TypeImage, layer.StatusExists)
		return nil
	}

	session := NewTUFSession(client, ref)

	img, err := session.GetImage()
	if err != nil {
		return err
	}

	layers, err := session.GetAncestors(img.ID())
	if err != nil {
		return err
	}

	descriptors := make([]xfer.DownloadDescriptor, 0, len(layers))
	newHistory := make([]image.History, 0, len(layers))
	for i := len(layers) - 1; i >= 0; i-- {
		l := layers[i]
		layerJSON, err := l.MarshalConfig()
		if err != nil {
			return err
		}
		history, err := v1.HistoryFromConfig(layerJSON, false)
		if err != nil {
			return err
		}
		newHistory = append(newHistory, history)
		descriptors = append(descriptors, l)
	}

	rootFS := image.NewRootFS()
	resultRootFS, release, err := c.downloadManager.Download(context.Background(), *rootFS, descriptors, NopProgress)
	if err != nil {
		return err
	}
	defer release()

	imgJSON, err := img.MarshalConfig()
	if err != nil {
		return err
	}
	config, err := v1.MakeConfigFromV1Config(imgJSON, &resultRootFS, newHistory)
	if err != nil {
		return err
	}

	imageID, err := c.imageStore.Create(config)
	if err != nil {
		return err
	}

	if err := c.referenceStore.AddTag(dockerRef, imageID, true); err != nil {
		return err
	}

	sendProgress(imageID.String(), layer.TypeImage, layer.StatusDownloaded)
	return nil
}

func (c *Context) Checkout(id string, imageID image.ID) (string, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	img, err := c.imageStore.Get(imageID)
	if err != nil {
		return "", err
	}
	id = "tmp-" + id
	rwLayer, err := c.layerStore.CreateRWLayer(id, img.RootFS.ChainID(), "", c.setupInitLayer, nil)
	if err != nil {
		return "", err
	}
	return rwLayer.Mount("")
}

func (c *Context) setupInitLayer(initPath string) error {
	return nil
}

func (c *Context) Cleanup(id string) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	id = "tmp-" + id
	rwLayer, err := c.layerStore.GetRWLayer(id)
	if err != nil {
		return err
	}
	if err := rwLayer.Unmount(); err != nil {
		return err
	}
	_, err = c.layerStore.ReleaseRWLayer(rwLayer)
	return err
}

var NopProgress = &nopProgress{}

type nopProgress struct{}

func (d *nopProgress) WriteProgress(progress.Progress) error {
	return nil
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

func DockerPullPrinter(out io.Writer) progress.Output {
	rd, wr := io.Pipe()
	progressOutput := streamformatter.NewJSONStreamFormatter().NewProgressOutput(wr, false)
	termOut, isTerm := term.GetFdInfo(out)
	go jsonmessage.DisplayJSONMessagesStream(rd, out, termOut, isTerm, nil)
	return progressOutput
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
		Retries:   tufutil.DefaultHTTPRetries,
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
