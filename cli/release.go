package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func init() {
	register("release", runRelease, `
usage: flynn release [-q|--quiet]
       flynn release add [-t <type>] [-f <file>] <uri>
       flynn release show [<id>]

Manage app releases.

Options:
	-q, --quiet        only print release IDs
	-t <type>          type of the release. Currently only 'docker' is supported. [default: docker]
	-f, --file=<file>  release configuration file

Commands:
	With no arguments, shows a list of releases associated with the app.

	add   add a new release

		Create a new release from a Docker image.

		The optional file argument takes a path to a file containing release
		configuration in a JSON format. It's primarily used for specifying the
		release environment and processes (similar to a Procfile). It can take any
		of the arguments the controller Release type can take.

	show  show information about a release

		Omit the ID to show information about the current release.

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
	$ flynn release add -f config.json https://registry.hub.docker.com?name=flynn/slugbuilder&id=15d72b7f573b
	Created release 427537e78be4417fae2e24d11bc993eb.

	$ flynn release
	ID                                Created
	427537e78be4417fae2e24d11bc993eb  11 seconds ago

	$ flynn release show
	ID:             427537e78be4417fae2e24d11bc993eb
	Artifact:       docker+https://registry.hub.docker.com?name=flynn/slugbuilder&id=15d72b7f573b
	Process Types:  echo
	Created At:     2015-05-06 21:58:12.751741 +0000 UTC
	ENV[MY_VAR]:    Hello World, this will be available in all process types.
`)
}

func runRelease(args *docopt.Args, client *controller.Client) error {
	if args.Bool["show"] {
		return runReleaseShow(args, client)
	}
	if args.Bool["add"] {
		if args.String["-t"] == "docker" {
			return runReleaseAddDocker(args, client)
		} else {
			return fmt.Errorf("Release type %s not supported.", args.String["-t"])
		}
	}
	return runReleaseList(args, client)
}

func runReleaseList(args *docopt.Args, client *controller.Client) error {
	list, err := client.AppReleaseList(mustApp())
	if err != nil {
		return err
	}

	if args.Bool["--quiet"] {
		for _, r := range list {
			fmt.Println(r.ID)
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()
	listRec(w, "ID", "Created")
	for _, r := range list {
		listRec(w, r.ID, humanTime(r.CreatedAt))
	}
	return nil
}

func runReleaseShow(args *docopt.Args, client *controller.Client) error {
	var release *ct.Release
	var err error
	if args.String["<id>"] != "" {
		release, err = client.GetRelease(args.String["<id>"])
	} else {
		release, err = client.GetAppRelease(mustApp())
	}
	if err != nil {
		return err
	}
	var artifactDesc string
	if release.ArtifactID != "" {
		artifact, err := client.GetArtifact(release.ArtifactID)
		if err != nil {
			return err
		}
		artifactDesc = fmt.Sprintf("%s+%s", artifact.Type, artifact.URI)
	}
	types := make([]string, 0, len(release.Processes))
	for typ := range release.Processes {
		types = append(types, typ)
	}
	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()
	listRec(w, "ID:", release.ID)
	listRec(w, "Artifact:", artifactDesc)
	listRec(w, "Process Types:", strings.Join(types, ", "))
	listRec(w, "Created At:", release.CreatedAt)
	for k, v := range release.Env {
		listRec(w, fmt.Sprintf("ENV[%s]", k), v)
	}
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

	if err := client.DeployAppRelease(mustApp(), release.ID); err != nil {
		return err
	}

	log.Printf("Created release %s.", release.ID)

	return nil
}
