package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"sort"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

const maxVersions = 5

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

// Add adds a version to the manifest.
//
// If the version is already in the manifest, an error is returned.
//
// The number of versions in the manifest is capped at the value of maxVersions.
func (m *FlynnManifest) Add(version, commit string) error {
	versions := make(sortVersions, 0, len(m.Versions)+1)
	for _, v := range m.Versions {
		if v.version() == version {
			return errors.New("version already in manifest")
		}
		versions = append(versions, v)
	}
	versions = append(versions, &FlynnVersion{Version: version, Commit: commit})
	sort.Sort(sort.Reverse(versions))
	m.Versions = make([]*FlynnVersion, 0, maxVersions)
	for i := 0; i < len(versions) && i < maxVersions; i++ {
		m.Versions = append(m.Versions, versions[i].(*FlynnVersion))
	}
	m.Current = m.Versions[0]
	return nil
}

type FlynnVersion struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

func (v *FlynnVersion) version() string {
	return v.Version
}

type Versioner interface {
	version() string
}

type sortVersions []Versioner

func (s sortVersions) Len() int {
	return len(s)
}

func (s sortVersions) Less(i, j int) bool {
	return bytes.Compare([]byte(s[i].version()), []byte(s[j].version())) < 0
}

func (s sortVersions) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
