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

func init() {
	register("release", runRelease, `
usage: flynn release add [-t <type>] [-f <file>] <uri>

Manage app releases.

Options:
	-t <type>          type of the release. Currently only 'docker' is supported. [default: docker]
	-f, --file <file>  release configuration file

Commands:
	add   add a new release

		Create a new release from a Docker image.

		The optional file argument takes a path to a file containing release
		configuration in a JSON format. It's primarily used for specifying the
		release environment and processes (similar to a Procfile). It can take any
		of the arguments the controller Release type can take.

Examples:

	Release an echo server using the flynn/slugbuilder image as a base, running socat.

	$ cat config.json
	{
		"env": {"MY_VAR": "Hello World, this will be available in all process types."},
		"processes": {
			"echo": {
				"cmd": ["socat -v tcp-l:$PORT,fork exec:/bin/cat"],
				"entrypoint": ["sh", "-c"],
				"env": {"ECHO": "This var is specific to the echo process type."},
				"ports": [{"proto": "tcp"}]
			}
		}
	}
	$ flynn release add -f config.json https://registry.hub.docker.com/flynn/slugbuilder?id=15d72b7f573b
	Created release f55fde802170.
`)
}

func runRelease(args *docopt.Args, client *controller.Client) error {
	if args.Bool["add"] {
		if args.String["-t"] == "docker" {
			return runReleaseAddDocker(args, client)
		} else {
			return fmt.Errorf("Release type %s not supported.", args.String["-t"])
		}
	}
	return fmt.Errorf("Top-level command not implemented.")
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

	if err := client.DeployAppRelease(mustApp(), release.ID); err != nil {
		return err
	}

	log.Printf("Created release %s.", release.ID)

	return nil
}
