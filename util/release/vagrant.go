package main

import (
	"encoding/json"
	"log"
	"os"

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
func (m *VagrantManifest) Add(version string, provider *VagrantProvider) {
	for _, v := range m.Versions {
		if v.Version == version {
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
	}

	m.Versions = append(m.Versions, &VagrantVersion{
		Version:   version,
		Providers: []*VagrantProvider{provider},
	})
}

type VagrantVersion struct {
	Version   string             `json:"version"`
	Providers []*VagrantProvider `json:"providers"`
}

type VagrantProvider struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	ChecksumType string `json:"checksum_type"`
	Checksum     string `json:"checksum"`
}
