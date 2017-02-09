package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"

	"github.com/flynn/flynn/builder/store"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/imagebuilder"
	"gopkg.in/inconshreveable/log15.v2"
)

func main() {
	if err := build(os.Args[1], os.Args[2]); err != nil {
		log.Fatal(err)
	}
}

func build(dir, name string) error {
	log := log15.New("component", "build-bootstrap")
	log.SetHandler(log15.StreamHandler(os.Stderr, log15.LogfmtFormat()))

	buildStore, err := store.NewLocalStore("/var/lib/flynn/local")
	if err != nil {
		log.Error("error initializing local store", "err", err)
		return err
	}

	tmp, err := ioutil.TempFile("", "flynn-squashfs")
	if err != nil {
		log.Error("error creating temp file", "err", err)
		return err
	}
	tmp.Close()

	layer, err := imagebuilder.Mksquashfs(dir, tmp.Name())
	if err != nil {
		log.Error("error creating squashfs layer", "err", err)
		os.Remove(tmp.Name())
		return err
	}
	layer.Meta = map[string]string{
		"flynn-build.name": name,
	}

	path := buildStore.(*store.LocalStore).LayerPath(layer.ID)
	if err := os.Rename(tmp.Name(), path); err != nil {
		log.Error("error renaming squashfs layer", "err", err)
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(path, 0644); err != nil {
		log.Error("error setting permissions", "err", err)
		os.Remove(path)
		return err
	}

	artifact := &ct.Artifact{
		Type: ct.ArtifactTypeFlynn,
		RawManifest: (&ct.ImageManifest{
			Type: ct.ImageManifestTypeV1,
			Rootfs: []*ct.ImageRootfs{{
				Platform: ct.DefaultImagePlatform,
				Layers:   []*ct.ImageLayer{layer},
			}},
		}).RawManifest(),
		LayerURLTemplate: buildStore.LayerURLTemplate(),
	}
	return json.NewEncoder(os.Stdout).Encode(artifact)
}
