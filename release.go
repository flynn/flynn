package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
)

var cmdReleaseAddDocker = &Command{
	Run:   runReleaseAddDocker,
	Usage: "release-add-docker <image> <tag>",
	Short: "add a docker image release",
	Long:  "Add a release referencing a Docker image",
}

func runReleaseAddDocker(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 2 {
		cmd.printUsage(true)
	}

	image := args[0]
	tag := args[1]

	if !strings.Contains(image, ".") {
		image = "/" + image
	}

	artifact := &ct.Artifact{
		Type: "docker",
		URI:  fmt.Sprintf("docker://%s?tag=%s", image, tag),
	}
	if err := client.CreateArtifact(artifact); err != nil {
		return err
	}

	release := &ct.Release{ArtifactID: artifact.ID}
	if err := client.CreateRelease(release); err != nil {
		return err
	}

	if err := client.SetAppRelease(mustApp(), release.ID); err != nil {
		return err
	}

	log.Printf("Created release %s.", release.ID)

	return nil
}
