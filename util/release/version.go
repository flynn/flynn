package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

func version(args *docopt.Args) {
	manifest := &FlynnManifest{}

	if err := json.NewDecoder(os.Stdin).Decode(manifest); err != nil {
		log.Fatal(err)
	}

	if err := manifest.Add(args.String["<version>"], args.String["<commit>"]); err != nil {
		log.Fatal(err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(manifest); err != nil {
		log.Fatal(err)
	}
}

type FlynnManifest struct {
	Current  *FlynnVersion   `json:"current"`
	Versions []*FlynnVersion `json:"versions"`
}

func (m *FlynnManifest) Add(version, commit string) error {
	for _, v := range m.Versions {
		if v.Version == version {
			return errors.New("version already in manifest")
		}
	}
	v := &FlynnVersion{Version: version, Commit: commit}
	// prepend the version so it's at the top
	m.Versions = append([]*FlynnVersion{v}, m.Versions...)
	m.Current = v
	return nil
}

type FlynnVersion struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}
