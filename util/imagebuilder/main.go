package main

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/docker/docker/pkg/archive"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pinkerton"
)

func main() {
	log.SetFlags(0)

	if len(os.Args) != 2 {
		log.Fatalf("usage: %s NAME", os.Args[0])
	}
	if err := build(os.Args[1]); err != nil {
		log.Fatalln("ERROR:", err)
	}
}

func build(name string) error {
	cmd := exec.Command("docker", "build", "-t", name, ".")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error building docker image: %s", err)
	}

	context, err := pinkerton.BuildContext("aufs", "/var/lib/docker")
	if err != nil {
		return err
	}

	layerDir := "/var/lib/flynn/layer-cache"
	if err := os.MkdirAll(layerDir, 0755); err != nil {
		return err
	}

	builder := &Builder{
		context:  context,
		layerDir: layerDir,
	}

	return builder.Build(name)
}

type Builder struct {
	context  *pinkerton.Context
	layerDir string
}

func (b *Builder) Build(name string) error {
	image, err := b.context.LookupImage(name)
	if err != nil {
		return err
	}

	history, err := b.context.History(name)
	if err != nil {
		return err
	}

	ids := make([]string, 0, len(history))
	layers := make([]*ct.ImageLayer, 0, len(history))
	for i := len(history) - 1; i >= 0; i-- {
		layer := history[i]
		ids = append(ids, layer.ID)
		if len(layer.Tags) > 0 {
			l, err := b.CreateLayer(ids)
			if err != nil {
				return err
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

	manifest := &ct.ImageManifest{
		Type:        ct.ImageManifestTypeV1,
		Entrypoints: map[string]*ct.ImageEntrypoint{"_default": entrypoint},
		Rootfs: []*ct.ImageRootfs{{
			Platform: ct.DefaultImagePlatform,
			Layers:   layers,
		}},
	}

	return json.NewEncoder(os.Stdout).Encode(manifest)
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
	layerJSON := filepath.Join(b.layerDir, imageID+".json")

	// acquire the lock file using flock(2) to synchronize access to the
	// layer JSON
	lockPath := layerJSON + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	defer os.Remove(lock.Name())
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	// if the layer JSON exists, deserialize and return
	f, err := os.Open(layerJSON)
	if err == nil {
		defer f.Close()
		var layer ct.ImageLayer
		return &layer, json.NewDecoder(f).Decode(&layer)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	// apply the docker layer diffs to a temporary directory
	dir, err := ioutil.TempDir("", "docker-layer-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	for i, id := range ids {
		parent := ""
		if i > 0 {
			parent = ids[i-1]
		}
		// TODO: AUFS whiteouts
		diff, err := b.context.Diff(id, parent)
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
	layer, err := b.mksquashfs(dir)
	if err != nil {
		return nil, err
	}

	// write the serialized layer to the JSON file
	f, err = os.Create(layerJSON)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(&layer); err != nil {
		os.Remove(layerJSON)
		return nil, err
	}
	return layer, nil
}

func (b *Builder) mksquashfs(dir string) (*ct.ImageLayer, error) {
	tmp, err := ioutil.TempFile("", "squashfs-")
	if err != nil {
		return nil, err
	}
	defer tmp.Close()

	if out, err := exec.Command("mksquashfs", dir, tmp.Name(), "-noappend").CombinedOutput(); err != nil {
		os.Remove(tmp.Name())
		return nil, fmt.Errorf("mksquashfs error: %s: %s", err, out)
	}

	h := sha512.New()
	length, err := io.Copy(h, tmp)
	if err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}

	sha512 := hex.EncodeToString(h.Sum(nil))
	dst := filepath.Join(b.layerDir, sha512+".squashfs")
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return nil, err
	}

	return &ct.ImageLayer{
		Type:       ct.ImageLayerTypeSquashfs,
		Length:     length,
		Mountpoint: "/",
		Hashes:     map[string]string{"sha512": sha512},
	}, nil
}
