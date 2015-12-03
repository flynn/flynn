package release

import (
	"bytes"
	"sort"
	"strings"
)

const maxVersions = 5

type EC2Manifest struct {
	Name     string        `json:"name"`
	Versions []*EC2Version `json:"versions"`
}

// Add adds an image to the manifest.
//
// If the version is already in the manifest, the given image either
// replaces any existing image with the same region, or is appended to
// the existing list of images for that version.
//
// If the version is not already in the manifest a new version is added
// containing the image.
//
// The number of versions in the manifest is capped at the value of maxVersions.
func (m *EC2Manifest) Add(version string, image *EC2Image) {
	versions := make(sortVersions, 0, len(m.Versions)+1)
	for _, v := range m.Versions {
		if v.version() == version {
			images := make([]*EC2Image, len(v.Images))
			added := false
			for n, i := range v.Images {
				if i.Region == image.Region {
					// replace existing image
					images[n] = image
					added = true
					continue
				}
				images[n] = i
			}
			if !added {
				images = append(images, image)
			}
			v.Images = images
			return
		}
		versions = append(versions, v)
	}
	versions = append(versions, &EC2Version{
		Version: version,
		Images:  []*EC2Image{image},
	})
	sort.Sort(sort.Reverse(versions))
	m.Versions = make([]*EC2Version, 0, maxVersions)
	for i := 0; i < len(versions) && i < maxVersions; i++ {
		m.Versions = append(m.Versions, versions[i].(*EC2Version))
	}
}

type EC2Version struct {
	Version string      `json:"version"`
	Images  []*EC2Image `json:"images"`
}

func (v *EC2Version) version() string {
	return v.Version
}

type EC2Image struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Region               string `json:"region"`
	OwnerID              string `json:"owner_id"`
	RootDeviceType       string `json:"root_device_type"`
	RootDeviceName       string `json:"root_device_name"`
	RootDeviceSnapshotID string `json:"root_device_snapshot_id"`
	VirtualizationType   string `json:"virtualization_type"`
	Hypervisor           string `json:"hypervisor"`
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
	// strip the leading "v" from the version as Vagrant only supports
	// versions like X.Y.Z, see https://github.com/flynn/flynn/issues/2230
	version = strings.TrimPrefix(version, "v")

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
