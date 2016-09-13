package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cliutil"
	"github.com/flynn/go-docopt"
	"gopkg.in/inconshreveable/log15.v2"
)

func run(cmd *exec.Cmd) {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func export(args *docopt.Args) {
	log := log15.New()

	log.Info("decoding manifest")
	var manifest map[string]*ct.Artifact
	if err := cliutil.DecodeJSONArg(args.String["<manifest>"], &manifest); err != nil {
		log.Error("error decoding manifest", "err", err)
		os.Exit(1)
	}

	exporter := Exporter{
		dir: args.String["<dir>"],
		log: log15.New(),
	}

	log.Info(fmt.Sprintf("exporting %d images to %s", len(manifest), exporter.dir))
	if err := exporter.Export(manifest); err != nil {
		log.Error("error exporting images", "err", err)
		os.Exit(1)
	}
}

type Exporter struct {
	dir string
	log log15.Logger
}

func (e *Exporter) Export(manifest map[string]*ct.Artifact) error {
	if err := os.MkdirAll(e.dir, 0755); err != nil {
		return err
	}

	for name, artifact := range manifest {
		e.log.Info("exporting image", "name", name)
		if err := e.exportImage(name, artifact); err != nil {
			e.log.Error("error exporting image", "name", name, "err", err)
			return err
		}
	}

	return nil
}

func (e *Exporter) exportImage(name string, artifact *ct.Artifact) error {
	log := e.log.New("name", name)

	for _, rootfs := range artifact.Manifest().Rootfs {
		for _, layer := range rootfs.Layers {
			log.Info("exporting layer", "id", layer.ID)
			if err := e.exportLayer(layer); err != nil {
				log.Error("error exporting layer", "id", layer.ID, "err", err)
				return err
			}
		}
	}

	path := e.imagePath(artifact.Manifest().ID())
	if _, err := os.Stat(path); err == nil {
		log.Info("manifest already exists")
		return nil
	}

	log.Info("writing image manifest", "path", path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Error("error writing image manifest", "path", path, "err", err)
		return err
	}
	if err := ioutil.WriteFile(path, artifact.RawManifest, 0644); err != nil {
		log.Error("error writing image manifest", "path", path, "err", err)
		return err
	}

	return nil
}

func (e *Exporter) exportLayer(layer *ct.ImageLayer) error {
	if _, err := os.Stat(e.layerPath(layer)); err == nil {
		e.log.Info("layer already exists", "id", layer.ID)
		return nil
	}

	if layer.Type != ct.ImageLayerTypeSquashfs {
		return fmt.Errorf("unknown layer type %q", layer.Type)
	}

	src, err := os.Open(filepath.Join("/var/lib/flynn/layer-cache", layer.ID+".squashfs"))
	if err != nil {
		return fmt.Errorf("error opening layer %q: %s", layer.ID, err)
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(e.layerPath(layer)), 0755); err != nil {
		return fmt.Errorf("error creating output layer: %s", err)
	}
	dst, err := os.Create(e.layerPath(layer))
	if err != nil {
		return fmt.Errorf("error creating output layer: %s", err)
	}
	defer dst.Close()

	n, err := io.Copy(dst, src)
	if err != nil {
		return err
	} else if n != layer.Length {
		return fmt.Errorf("error copying layer: expected to write %d bytes but copied %d", layer.Length, n)
	}

	return nil
}

func (e *Exporter) imagePath(id string) string {
	return filepath.Join(e.dir, "images", id+".json")
}

func (e *Exporter) layerPath(layer *ct.ImageLayer) string {
	return filepath.Join(e.dir, "layers", layer.ID+".squashfs")
}
