package main

import (
	"encoding/json"
	"io/ioutil"
	"log"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

var cmdReleaseAddDocker = &Command{
	Run:   runReleaseAddDocker,
	Usage: "release-add-docker <uri>",
	Short: "add a docker image release",
	Long:  "Add a release referencing a Docker image",
}

var releaseFile string

func init() {
	cmdReleaseAddDocker.Flag.StringVarP(&releaseFile, "file", "f", "", "path to JSON release config")
}

func runReleaseAddDocker(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 1 {
		cmd.printUsage(true)
	}

	release := &ct.Release{}
	if releaseFile != "" {
		data, err := ioutil.ReadFile(releaseFile)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, release); err != nil {
			return err
		}
	}

	artifact := &ct.Artifact{
		Type: "docker",
		URI:  args[0],
	}
	if err := client.CreateArtifact(artifact); err != nil {
		return err
	}

	release.ArtifactID = artifact.ID
	if err := client.CreateRelease(release); err != nil {
		return err
	}

	if err := client.SetAppRelease(mustApp(), release.ID); err != nil {
		return err
	}

	log.Printf("Created release %s.", release.ID)

	return nil
}
