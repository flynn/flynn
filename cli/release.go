package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func runRelease(argv []string, client *controller.Client) error {
	usage := `usage: flynn release add [-t <type>] <uri>

Manage app releases.

Options:
   -t <type>          type of the release. Currently only 'docker' is supported. [default: docker]
   -f, --file <file>  add a release referencing a Docker image
Commands:
   add   add a new release
	`
	args, _ := docopt.Parse(usage, argv, true, "", false)

	if args.Bool["add"] {
		if args.String["-t"] == "docker" {
			return runReleaseAddDocker(args, client)
		} else {
			return fmt.Errorf("Release type %s not supported.", args.String["-t"])
		}
	}

	log.Fatal("Toplevel command not implemented.")
	return nil
}

func runReleaseAddDocker(args *docopt.Args, client *controller.Client) error {
	release := &ct.Release{}
	if args.String["--file"] != "" {
		data, err := ioutil.ReadFile(args.String["--file"])
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, release); err != nil {
			return err
		}
	}

	artifact := &ct.Artifact{
		Type: "docker",
		URI:  args.String["<uri>"],
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
