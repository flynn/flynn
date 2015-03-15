package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/ec2"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/util/release/types"
)

func amis(args *docopt.Args) {
	auth, err := aws.EnvAuth()
	if err != nil {
		log.Fatal(err)
	}

	manifest := &release.EC2Manifest{}

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

		manifest.Add(args.String["<version>"], &release.EC2Image{
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
