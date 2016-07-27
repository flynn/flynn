package imagebuilder

import (
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/docker/docker/pkg/archive"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pinkerton"
)

type Builder struct {
	Store   LayerStore
	Context *pinkerton.Context
}

func (b *Builder) Build(name string, groupByTags bool) (*ct.ImageManifest, error) {
	image, err := b.Context.LookupImage(name)
	if err != nil {
		return nil, err
	}

	history, err := b.Context.History(name)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(history))
	layers := make([]*ct.ImageLayer, 0, len(history))
	for i := len(history) - 1; i >= 0; i-- {
		layer := history[i]
		ids = append(ids, layer.ID)
		if !groupByTags || len(layer.Tags) > 0 {
			l, err := b.CreateLayer(ids)
			if err != nil {
				return nil, err
			}
			ids = make([]string, 0, len(history))
			layers = append(layers, l)
		}
	}

	entrypoint := &ct.ImageEntrypoint{
		WorkingDir: image.Config.WorkingDir,
		Env:        make(map[string]string, len(image.Config.Env)),
		Args:       append(image.Config.Entrypoint.Slice(), image.Config.Cmd.Slice()...),
	}
	for _, env := range image.Config.Env {
		keyVal := strings.SplitN(env, "=", 2)
		if len(keyVal) != 2 {
			continue
		}
		entrypoint.Env[keyVal[0]] = keyVal[1]
	}

	return &ct.ImageManifest{
		Type:        ct.ImageManifestTypeV1,
		Entrypoints: map[string]*ct.ImageEntrypoint{"_default": entrypoint},
		Rootfs: []*ct.ImageRootfs{{
			Platform: ct.DefaultImagePlatform,
			Layers:   layers,
		}},
	}, nil
}

type LayerStore interface {
	Load(id string) (*ct.ImageLayer, error)
	Save(id, path string, layer *ct.ImageLayer) error
}

type LayerLocker interface {
	Lock(id string, f func() error) error
}

// CreateLayer creates a squashfs layer from a docker layer ID chain by
// creating a temporary directory, applying the relevant diffs then calling
// mksquashfs.
//
// Each squashfs layer is serialized as JSON and cached in a temporary file to
// avoid regenerating existing layers, with access wrapped with a lock file in
// case multiple images are being built at the same time.
func (b *Builder) CreateLayer(ids []string) (*ct.ImageLayer, error) {
	imageID := ids[len(ids)-1]

	locker, ok := b.Store.(LayerLocker)
	if !ok {
		return b.createLayer(ids)
	}
	var layer *ct.ImageLayer
	err := locker.Lock(imageID, func() (err error) {
		layer, err = b.createLayer(ids)
		return
	})
	if err != nil {
		return nil, err
	}
	return layer, nil

}

func (b *Builder) createLayer(ids []string) (*ct.ImageLayer, error) {
	imageID := ids[len(ids)-1]

	layer, err := b.Store.Load(imageID)
	if err != nil {
		return nil, err
	} else if layer != nil {
		return layer, nil
	}

	// apply the docker layer diffs to a temporary directory
	dir, err := ioutil.TempDir("", "docker-layer-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	for _, id := range ids {
		// TODO: AUFS whiteouts
		diff, err := b.Context.Diff(id, "")
		if err != nil {
			return nil, err
		}
		if err := archive.Untar(diff, dir, &archive.TarOptions{}); err != nil {
			return nil, err
		}
	}

	// create the squashfs layer, with the root dir having 755 permissions
	if err := os.Chmod(dir, 0755); err != nil {
		return nil, err
	}
	path, layer, err := b.mksquashfs(dir)
	if err != nil {
		return nil, err
	}

	return layer, b.Store.Save(imageID, path, layer)
}

func (b *Builder) mksquashfs(dir string) (string, *ct.ImageLayer, error) {
	tmp, err := ioutil.TempFile("", "squashfs-")
	if err != nil {
		return "", nil, err
	}
	defer tmp.Close()

	if out, err := exec.Command("mksquashfs", dir, tmp.Name(), "-noappend").CombinedOutput(); err != nil {
		os.Remove(tmp.Name())
		return "", nil, fmt.Errorf("mksquashfs error: %s: %s", err, out)
	}

	h := sha512.New()
	length, err := io.Copy(h, tmp)
	if err != nil {
		os.Remove(tmp.Name())
		return "", nil, err
	}

	sha512 := hex.EncodeToString(h.Sum(nil))
	return tmp.Name(), &ct.ImageLayer{
		ID:     sha512,
		Type:   ct.ImageLayerTypeSquashfs,
		Length: length,
		Hashes: map[string]string{"sha512": sha512},
	}, nil
}
