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
       flynn release update <file> [<id>] [--clean]
       flynn release show [--json] [<id>]

Manage app releases.

Options:
	-q, --quiet        only print release IDs
	-t <type>          type of the release. Currently only 'docker' is supported. [default: docker]
	-f, --file=<file>  release configuration file
	--json             print release configuration in JSON format
	--clean            update from a clean slate (ignoring prior config)

Commands:
	With no arguments, shows a list of releases associated with the app.

	add	add a new release

		Create a new release from a Docker image.

		The optional file argument takes a path to a file containing release
		configuration in a JSON format. It's primarily used for specifying the
		release environment and processes (similar to a Procfile). It can take any
		of the arguments the controller Release type can take.

	show	show information about a release

		Omit the ID to show information about the current release.

	update	update an existing release

		Takes a path to a file containing release configuration in a JSON format.
		It can take any of the arguments the controller Release type can take, and
		will override existing config with any values set thus. Omit the ID to
		update the current release.

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

	$ cat update.json
	{
		"processes": {
			"echo": {
				"omni": true
			}
		}
	}
	$ flynn release update 427537e78be4417fae2e24d11bc993eb update.json
	Created release 0101020305080d1522375990e9000000.

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
	if args.Bool["update"] {
		return runReleaseUpdate(args, client)
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
	if args.Bool["--json"] {
		return json.NewEncoder(os.Stdout).Encode(release)
	}
	var artifactDesc string
	if release.ImageArtifactID != "" {
		artifact, err := client.GetArtifact(release.ImageArtifactID)
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

	release.ImageArtifactID = artifact.ID
	if err := client.CreateRelease(release); err != nil {
		return err
	}

	if err := client.DeployAppRelease(mustApp(), release.ID); err != nil {
		return err
	}

	log.Printf("Created release %s.", release.ID)

	return nil
}

func runReleaseUpdate(args *docopt.Args, client *controller.Client) error {
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

	updates := &ct.Release{}
	data, err := ioutil.ReadFile(args.String["<file>"])
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, updates); err != nil {
		return err
	}

	// Basically, there's no way to merge JSON that can reliably knock out set values.
	// Instead, throw the --clean flag to start from a largely empty Release.
	if args.Bool["--clean"] {
		updates.ImageArtifactID = release.ImageArtifactID
		release = updates
	} else {
		release.ID = ""
		for key, value := range updates.Env {
			release.Env[key] = value
		}
		for key, value := range updates.Meta {
			release.Meta[key] = value
		}
		for procKey, procUpdate := range updates.Processes {
			procRelease, ok := release.Processes[procKey]
			if !ok {
				release.Processes[procKey] = procUpdate
				continue
			}

			if len(procUpdate.Cmd) > 0 {
				procRelease.Cmd = procUpdate.Cmd
			}
			if len(procUpdate.Entrypoint) > 0 {
				procRelease.Entrypoint = procUpdate.Entrypoint
			}
			for key, value := range procUpdate.Env {
				procRelease.Env[key] = value
			}
			if len(procUpdate.Ports) > 0 {
				procRelease.Ports = procUpdate.Ports
			}
			if procUpdate.Data {
				procRelease.Data = true
			}
			if procUpdate.Omni {
				procRelease.Omni = true
			}
			if procUpdate.HostNetwork {
				procRelease.HostNetwork = true
			}
			if len(procUpdate.Service) > 0 {
				procRelease.Service = procUpdate.Service
			}
			if procUpdate.Resurrect {
				procRelease.Resurrect = true
			}
			for resKey, resValue := range procUpdate.Resources {
				procRelease.Resources[resKey] = resValue
			}

			release.Processes[procKey] = procRelease
		}
	}

	if err := client.CreateRelease(release); err != nil {
		return err
	}

	if err := client.DeployAppRelease(mustApp(), release.ID); err != nil {
		return err
	}

	log.Printf("Created release %s.", release.ID)

	return nil
}
