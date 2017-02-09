package main

import (
	"encoding/json"
	"os"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/go-docopt"
)

var cmdArtifact = Command{
	Run: runArtifact,
	Usage: `
usage: flynn-build artifact <base> <layers>...

Build an image artifact from a list of image layers.
`[1:],
}

func runArtifact(args *docopt.Args) error {
	paths := args.All["<layers>"].([]string)
	layers := make([]*ct.ImageLayer, len(paths))
	for i, path := range paths {
		layer, err := loadLayer(path)
		if err != nil {
			return err
		}
		layers[i] = layer
	}
	artifact, err := loadArtifact(args.String["<base>"])
	if err != nil {
		return err
	}
	manifest := artifact.Manifest()
	manifest.Rootfs[0].Layers = append(manifest.Rootfs[0].Layers, layers...)
	if entrypoint := loadEntrypoint(); entrypoint != nil {
		manifest.Entrypoints = map[string]*ct.ImageEntrypoint{"_default": entrypoint}
	}
	artifact.RawManifest = manifest.RawManifest()
	artifact.Hashes = manifest.Hashes()
	artifact.Size = int64(len(artifact.RawManifest))
	return json.NewEncoder(os.Stdout).Encode(artifact)
}

func loadLayer(path string) (*ct.ImageLayer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var layer ct.ImageLayer
	return &layer, json.NewDecoder(f).Decode(&layer)
}

func loadEntrypoint() *ct.ImageEntrypoint {
	f, err := os.Open("img/entrypoint.json")
	if err != nil {
		return nil
	}
	defer f.Close()
	entrypoint := &ct.ImageEntrypoint{}
	if err := json.NewDecoder(f).Decode(entrypoint); err != nil {
		return nil
	}
	return entrypoint
}
