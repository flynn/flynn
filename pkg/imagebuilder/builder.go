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
			// only add non-empty layers
			if l != nil {
				layers = append(layers, l)
			}
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
		val := strings.Replace(keyVal[1], "\t", "\\t", -1)
		entrypoint.Env[keyVal[0]] = val
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
	err := locker.DoLocked(imageID, func() (err error) {
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
		diff, err := b.Context.Diff(id, "")
		if err != nil {
			return nil, err
		}
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

	// skip creating the layer if the diff is empty (e.g the result of an
	// ENV step in a Dockerfile)
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	} else if len(files) == 0 {
		return nil, nil
	}

	// create the squashfs layer, with the root dir having 755 permissions
	if err := os.Chmod(dir, 0755); err != nil {
		return nil, err
	}
	path, layer, err := b.mksquashfs(dir)
	if err != nil {
		return nil, err
	}
	layer.Meta = map[string]string{
		"docker.layer_id": imageID,
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
