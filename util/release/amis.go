package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"

	"github.com/awslabs/aws-sdk-go/aws"
	"github.com/awslabs/aws-sdk-go/gen/ec2"
	"github.com/flynn/flynn/util/release/types"
	"github.com/flynn/go-docopt"
)

func amis(args *docopt.Args) {
	auth, err := aws.EnvCreds()
	if err != nil {
		log.Fatal(err)
	}

	manifest := &release.EC2Manifest{}

	if err := json.NewDecoder(os.Stdin).Decode(manifest); err != nil {
		log.Fatal(err)
	}

	for _, s := range strings.Split(args.String["<ids>"], ",") {
		regionID := strings.SplitN(s, ":", 2)
		svc := ec2.New(auth, regionID[0], nil)
		resp, err := svc.DescribeImages(&ec2.DescribeImagesRequest{ImageIDs: []string{regionID[1]}})
		if err != nil {
			log.Fatal(err)
		}
		if len(resp.Images) < 1 {
			log.Fatalln("Could not find image", regionID[1])
		}
		image := resp.Images[0]

		var snapshotID string
		for _, mapping := range image.BlockDeviceMappings {
			if *mapping.DeviceName == *image.RootDeviceName {
				snapshotID = *mapping.EBS.SnapshotID
			}
		}
		if snapshotID == "" {
			log.Fatalln("Could not determine RootDeviceSnapshotID for", regionID[1])
		}

		manifest.Add(args.String["<version>"], &release.EC2Image{
			ID:                   *image.ImageID,
			Name:                 *image.Name,
			Region:               regionID[0],
			OwnerID:              *image.OwnerID,
			RootDeviceType:       *image.RootDeviceType,
			RootDeviceName:       *image.RootDeviceName,
			RootDeviceSnapshotID: snapshotID,
			VirtualizationType:   *image.VirtualizationType,
			Hypervisor:           *image.Hypervisor,
		})

	}

	if err := json.NewEncoder(os.Stdout).Encode(manifest); err != nil {
		log.Fatal(err)
	}
}
