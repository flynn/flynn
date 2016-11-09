package imagebuilder

import (
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/docker/docker/pkg/archive"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pinkerton"
)

type Builder struct {
	Store   LayerStore
	Context *pinkerton.Context
}

// Build builds a Flynn image from a Docker image, either creating a squashfs
// layer per Docker layer or a squashfs layer for a group of layers up to a
// Docker tag (e.g. slugrunner becomes three squashfs layers for ubuntu ->
// cedarish -> slugrunner when groupByTags is set)
func (b *Builder) Build(name string, groupByTags bool) (*ct.ImageManifest, error) {
	image, err := b.Context.LookupImage(name)
	if err != nil {
		return nil, err
	}

	history, err := b.Context.History(image)
	if err != nil {
		return nil, err
	}

	layers := make([]*ct.ImageLayer, 0, len(history))
	from := 0
	for i, layer := range history {
		if !groupByTags || len(layer.Tags) > 0 {
			l, err := b.CreateLayer(history[from : i+1])
			if err != nil {
				return nil, err
			}
			layers = append(layers, l)
			from = i
		}
	}

	entrypoint := &ct.ImageEntrypoint{
		WorkingDir: image.Config.WorkingDir,
		Env:        make(map[string]string, len(image.Config.Env)),
		Args:       append(image.Config.Entrypoint, image.Config.Cmd...),
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
	DoLocked(id string, f func() error) error
}

// CreateLayer creates a squashfs layer from a list of docker layers by
// creating a temporary directory, applying the relevant diffs then calling
// mksquashfs.
//
// Each squashfs layer is serialized as JSON and cached in a temporary file to
// avoid regenerating existing layers, with access wrapped with a lock file in
// case multiple images are being built at the same time.
func (b *Builder) CreateLayer(dockerLayers []*pinkerton.DockerLayer) (*ct.ImageLayer, error) {
	imageID := dockerLayers[len(dockerLayers)-1].ID

	locker, ok := b.Store.(LayerLocker)
	if !ok {
		return b.createLayer(dockerLayers)
	}
	var layer *ct.ImageLayer
	err := locker.DoLocked(imageID, func() (err error) {
		layer, err = b.createLayer(dockerLayers)
		return
	})
	if err != nil {
		return nil, err
	}
	return layer, nil

}

func (b *Builder) createLayer(dockerLayers []*pinkerton.DockerLayer) (*ct.ImageLayer, error) {
	imageID := dockerLayers[len(dockerLayers)-1].ID

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
	for _, l := range dockerLayers {
		if l.ChainID == "" {
			continue
		}
		diff, err := b.Context.Diff(l.ChainID)
		if err != nil {
			return nil, err
		}
		defer diff.Close()

		if err := archive.Untar(diff, dir, &archive.TarOptions{}); err != nil {
			return nil, err
		}

		// convert Docker AUFS whiteouts to overlay whiteouts.
		//
		// See the "whiteouts and opaque directories" section of the
		// OverlayFS documentation for a description of the whiteout
		// file formats:
		// https://www.kernel.org/doc/Documentation/filesystems/overlayfs.txt
		err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}

			base := filepath.Base(path)
			dir := filepath.Dir(path)

			if base == archive.WhiteoutOpaqueDir {
				if err := syscall.Setxattr(dir, "trusted.overlay.opaque", []byte{'y'}, 0); err != nil {
					return err
				}
				return os.Remove(path)
			}

			if !strings.HasPrefix(base, archive.WhiteoutPrefix) {
				return nil
			}

			// replace the file which the AUFS whiteout is hiding
			// with an overlay whiteout file, and remove the AUFS
			// whiteout
			name := filepath.Join(dir, strings.TrimPrefix(base, archive.WhiteoutPrefix))
			if err := os.RemoveAll(name); err != nil {
				return err
			}
			if err := syscall.Mknod(name, syscall.S_IFCHR, 0); err != nil {
				return err
			}
			stat := info.Sys().(*syscall.Stat_t)
			if err := os.Chown(name, int(stat.Uid), int(stat.Gid)); err != nil {
				return err
			}
			return os.Remove(path)
		})
		if err != nil {
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

	h := sha512.New512_256()
	length, err := io.Copy(h, tmp)
	if err != nil {
		os.Remove(tmp.Name())
		return "", nil, err
	}

	sha := hex.EncodeToString(h.Sum(nil))

	return tmp.Name(), &ct.ImageLayer{
		ID:     sha,
		Type:   ct.ImageLayerTypeSquashfs,
		Length: length,
		Hashes: map[string]string{"sha512_256": sha},
	}, nil
}
