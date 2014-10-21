package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/ec2"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

func amis(args *docopt.Args) {
	auth, err := aws.EnvAuth()
	if err != nil {
		log.Fatal(err)
	}

	manifest := &EC2Manifest{}

	if err := json.NewDecoder(os.Stdin).Decode(manifest); err != nil {
		log.Fatal(err)
	}

	for _, s := range strings.Split(args.String["<ids>"], ",") {
		regionID := strings.SplitN(s, ":", 2)
		resp, err := ec2.New(auth, aws.Regions[regionID[0]]).Images([]string{regionID[1]}, nil)
		if err != nil {
			log.Fatal(err)
		}
		if len(resp.Images) < 1 {
			log.Fatalln("Could not find image", regionID[1])
		}
		image := resp.Images[0]

		var snapshotID string
		for _, mapping := range image.BlockDevices {
			if mapping.DeviceName == image.RootDeviceName {
				snapshotID = mapping.SnapshotId
			}
		}
		if snapshotID == "" {
			log.Fatalln("Could not determine RootDeviceSnapshotID for", regionID[1])
		}

		manifest.Add(args.String["<version>"], &EC2Image{
			ID:                   image.Id,
			Name:                 image.Name,
			Region:               regionID[0],
			OwnerID:              image.OwnerId,
			RootDeviceType:       image.RootDeviceType,
			RootDeviceName:       image.RootDeviceName,
			RootDeviceSnapshotID: snapshotID,
			VirtualizationType:   image.VirtualizationType,
			Hypervisor:           image.Hypervisor,
		})

	}

	if err := json.NewEncoder(os.Stdout).Encode(manifest); err != nil {
		log.Fatal(err)
	}
}

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
func (m *EC2Manifest) Add(version string, image *EC2Image) {
	for _, v := range m.Versions {
		if v.Version == version {
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
	}

	m.Versions = append(m.Versions, &EC2Version{
		Version: version,
		Images:  []*EC2Image{image},
	})
}

type EC2Version struct {
	Version string      `json:"version"`
	Images  []*EC2Image `json:"images"`
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
