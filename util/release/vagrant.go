package main

import (
	"encoding/json"
	"log"
	"os"
	"sort"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

func vagrant(args *docopt.Args) {
	manifest := &VagrantManifest{}

	if err := json.NewDecoder(os.Stdin).Decode(manifest); err != nil {
		log.Fatal(err)
	}

	manifest.Add(args.String["<version>"], &VagrantProvider{
		Name:         args.String["<provider>"],
		URL:          args.String["<url>"],
		Checksum:     args.String["<checksum>"],
		ChecksumType: "sha256",
	})

	if err := json.NewEncoder(os.Stdout).Encode(manifest); err != nil {
		log.Fatal(err)
	}
}

type VagrantManifest struct {
	Name     string            `json:"name"`
	Versions []*VagrantVersion `json:"versions"`
}

// Add adds a provider to the manifest.
//
// If the version is already in the manifest, the given provider either
// replaces any existing provider with the same name, or is appended to
// the existing list of providers for that version.
//
// If the version is not already in the manifest a new version is added
// containing the provider.
//
// The number of versions in the manifest is capped at the value of maxVersions.
func (m *VagrantManifest) Add(version string, provider *VagrantProvider) {
	versions := make(sortVersions, 0, len(m.Versions)+1)
	for _, v := range m.Versions {
		if v.version() == version {
			providers := make([]*VagrantProvider, len(v.Providers))
			added := false
			for i, p := range v.Providers {
				if p.Name == provider.Name {
					// replace existing provider
					providers[i] = provider
					added = true
					continue
				}
				providers[i] = p
			}
			if !added {
				providers = append(providers, provider)
			}
			v.Providers = providers
			return
		}
		versions = append(versions, v)
	}
	versions = append(versions, &VagrantVersion{
		Version:   version,
		Providers: []*VagrantProvider{provider},
	})
	sort.Sort(sort.Reverse(versions))
	m.Versions = make([]*VagrantVersion, 0, maxVersions)
	for i := 0; i < len(versions) && i < maxVersions; i++ {
		m.Versions = append(m.Versions, versions[i].(*VagrantVersion))
	}
}

type VagrantVersion struct {
	Version   string             `json:"version"`
	Providers []*VagrantProvider `json:"providers"`
}

func (v *VagrantVersion) version() string {
	return v.Version
}

type VagrantProvider struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	ChecksumType string `json:"checksum_type"`
	Checksum     string `json:"checksum"`
}
