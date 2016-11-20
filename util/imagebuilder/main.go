package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pkg/imagebuilder"
)

var dir = flag.String("dir", ".", "directory to build")

func main() {
	log.SetFlags(0)
	flag.Parse()

	if len(flag.Args()) != 1 {
		log.Fatalf("usage: %s [--dir DIR] NAME", os.Args[0])
	}
	if err := build(flag.Args()[0], *dir); err != nil {
		log.Fatalln("ERROR:", err)
	}
}

func build(name, dir string) error {
	name = "flynn/" + name

	cmd := exec.Command("docker", "build", "-t", name, dir)
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

	b := &imagebuilder.Builder{
		Store:   &layerStore{layerDir},
		Context: context,
	}

	manifest, err := b.Build(name, true)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(manifest.RawManifest())
	return err
}

type layerStore struct {
	root string
}

func (l *layerStore) DoLocked(id string, fn func() error) error {
	f, err := os.OpenFile(l.lockPath(id), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func (l *layerStore) Load(id string) (*ct.ImageLayer, error) {
	f, err := os.Open(l.jsonPath(id))
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	defer f.Close()
	var layer ct.ImageLayer
	return &layer, json.NewDecoder(f).Decode(&layer)
}

func (l *layerStore) Save(id, path string, layer *ct.ImageLayer) error {
	if err := os.Rename(path, l.layerPath(layer)); err != nil {
		return err
	}
	if err := os.Chmod(l.layerPath(layer), 0644); err != nil {
		return err
	}
	f, err := os.Create(l.jsonPath(id))
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(&layer); err != nil {
		os.Remove(l.jsonPath(id))
		return err
	}
	return nil
}

func (l *layerStore) lockPath(id string) string {
	return l.jsonPath(id) + ".json"
}

func (l *layerStore) jsonPath(id string) string {
	return filepath.Join(l.root, id+".json")
}

func (l *layerStore) layerPath(layer *ct.ImageLayer) string {
	return filepath.Join(l.root, layer.ID+".squashfs")
}
