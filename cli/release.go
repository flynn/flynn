package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/go-docopt"
)

func init() {
	register("release", runRelease, `
usage: flynn release [-q|--quiet]
       flynn release add [-t <type>] [-f <file>] <uri>
       flynn release update <file> [<id>] [--clean]
       flynn release show [--json] [<id>]
       flynn release delete [-y] <id>
       flynn release rollback [-y] [<id>]

Manage app releases.

Options:
	-q, --quiet        only print release IDs
	-t <type>          type of the release. Currently only 'docker' is supported. [default: docker]
	-f, --file=<file>  release configuration file
	--json             print release configuration in JSON format
	--clean            update from a clean slate (ignoring prior config)
	-y, --yes          skip the confirmation prompt when deleting a release

Commands:
	With no arguments, shows a list of releases associated with the app.

	add
		DEPRECATED: Only works on legacy clusters.

		Create a new release from a Docker image.

		The optional file argument takes a path to a file containing release
		configuration in a JSON format. It's primarily used for specifying the
		release environment and processes (similar to a Procfile). It can take any
		of the arguments the controller Release type can take.

	show
		Show information about a release.

		Omit the ID to show information about the current release.

	update
		Update an existing release.

		Takes a path to a file containing release configuration in a JSON format.
		It can take any of the arguments the controller Release type can take, and
		will override existing config with any values set thus. Omit the ID to
		update the current release.

	delete
		Delete a release.

		Any associated file artifacts (e.g. slugs) will also be deleted.

	rollback
		Rollback to a previous release. Deploys the previous release or specified release ID.

Examples:

	Release an echo server using the flynn/slugbuilder image as a base, running socat.

	$ cat config.json
	{
		"env": {"MY_VAR": "Hello World, this will be available in all process types."},
		"processes": {
			"echo": {
				"args": ["sh", "-c", "socat -v tcp-l:$PORT,fork exec:/bin/cat"],
				"env": {"ECHO": "This var is specific to the echo process type."},
				"ports": [{"proto": "tcp"}]
			}
		}
	}
	$ flynn release add -f config.json https://registry.hub.docker.com?name=flynn/slugbuilder&id=15d72b7f573b
	Created release 989ce4a8-0088-444c-8379-caddded4b957.

	$ flynn release
	ID                                Created
	989ce4a8-0088-444c-8379-caddded4b957  11 seconds ago

	$ flynn release show
	ID:             989ce4a8-0088-444c-8379-caddded4b957
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
	$ flynn release update update.json
	Created release 1a270395-8d31-4ec1-953a-0683b4f12635.

	$ flynn release delete --yes c6b7f512-ef49-46f7-bb57-dd39e97bfb09
	Deleted release c6b7f512-ef49-46f7-bb57-dd39e97bfb09 (deleted 1 files)
`)
}

func runRelease(args *docopt.Args, client controller.Client) error {
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
	if args.Bool["delete"] {
		return runReleaseDelete(args, client)
	}
	if args.Bool["rollback"] {
		return runReleaseRollback(args, client)
	}
	return runReleaseList(args, client)
}

func runReleaseList(args *docopt.Args, client controller.Client) error {
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

func runReleaseShow(args *docopt.Args, client controller.Client) error {
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
	var artifacts []string
	for _, id := range release.ArtifactIDs {
		artifact, err := client.GetArtifact(id)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, fmt.Sprintf("%s+%s", artifact.Type, artifact.URI))
	}
	types := make([]string, 0, len(release.Processes))
	for typ := range release.Processes {
		types = append(types, typ)
	}
	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()
	listRec(w, "ID:", release.ID)
	for i, artifact := range artifacts {
		listRec(w, fmt.Sprintf("Artifact[%d]:", i), artifact)
	}
	listRec(w, "Process Types:", strings.Join(types, ", "))
	listRec(w, "Created At:", release.CreatedAt)
	for k, v := range release.Env {
		listRec(w, fmt.Sprintf("ENV[%s]", k), v)
	}
	return nil
}

func runReleaseAddDocker(args *docopt.Args, client controller.Client) error {
	fmt.Fprintln(os.Stderr, "WARN: The 'release add' command is deprecated and only works on legacy clusters, use 'docker push' to push Docker images")

	app, err := client.GetApp(mustApp())
	if err != nil {
		return err
	}
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
		Type: ct.DeprecatedArtifactTypeDocker,
		URI:  args.String["<uri>"],
	}
	if err := client.CreateArtifact(artifact); err != nil {
		return err
	}

	release.ArtifactIDs = []string{artifact.ID}
	if err := client.CreateRelease(app.ID, release); err != nil {
		return err
	}

	if err := client.DeployAppRelease(app.ID, release.ID, nil); err != nil {
		return err
	}

	log.Printf("Created release %s.", release.ID)

	return nil
}

func runReleaseUpdate(args *docopt.Args, client controller.Client) error {
	app, err := client.GetApp(mustApp())
	if err != nil {
		return err
	}
	var release *ct.Release
	if args.String["<id>"] != "" {
		release, err = client.GetRelease(args.String["<id>"])
	} else {
		release, err = client.GetAppRelease(app.ID)
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
		updates.ArtifactIDs = release.ArtifactIDs
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

			if len(procUpdate.Args) > 0 {
				procRelease.Args = procUpdate.Args
			}
			for key, value := range procUpdate.Env {
				procRelease.Env[key] = value
			}
			if len(procUpdate.Ports) > 0 {
				procRelease.Ports = procUpdate.Ports
			}
			if len(procUpdate.Volumes) > 0 {
				procRelease.Volumes = procUpdate.Volumes
			}
			if procUpdate.DeprecatedData {
				fmt.Fprintln(os.Stderr, `WARN: ProcessType.Data is deprecated and will be removed in future versions, populate ProcessType.Volumes instead e.g. "volumes": [{"path": "/data"}]`)
				procRelease.DeprecatedData = true
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

	if err := client.CreateRelease(app.ID, release); err != nil {
		return err
	}

	if err := client.DeployAppRelease(app.ID, release.ID, nil); err != nil {
		return err
	}

	log.Printf("Created release %s.", release.ID)

	return nil
}

func runReleaseDelete(args *docopt.Args, client controller.Client) error {
	releaseID := args.String["<id>"]
	if !args.Bool["--yes"] {
		if !promptYesNo(fmt.Sprintf("Are you sure you want to delete release %q?", releaseID)) {
			return nil
		}
	}
	res, err := client.DeleteRelease(mustApp(), releaseID)
	if err != nil {
		return err
	}
	if len(res.RemainingApps) > 0 {
		log.Printf("Release scaled down for app but not fully deleted (still associated with %d other apps)", len(res.RemainingApps))
	} else {
		log.Printf("Deleted release %s (deleted %d files)", releaseID, len(res.DeletedFiles))
	}
	return nil
}

func runReleaseRollback(args *docopt.Args, client controller.Client) error {
	currentRelease, err := client.GetAppRelease(mustApp())
	if err != nil {
		return err
	}
	releaseID := args.String["<id>"]
	if releaseID == "" {
		releases, err := client.AppReleaseList(mustApp())
		if err != nil {
			return err
		}
		if len(releases) < 2 {
			return fmt.Errorf("Not enough releases to perform a rollback.")
		}
		releaseID = releases[1].ID
	} else if releaseID == currentRelease.ID {
		return fmt.Errorf("Release id given is the current release.")
	}

	if !args.Bool["--yes"] {
		if !promptYesNo(fmt.Sprintf("Are you sure you want to rollback to release %q?", releaseID)) {
			return nil
		}
	}

	log.Printf("Rolling back to release %s from %s.\n", releaseID, currentRelease.ID)

	if err := client.DeployAppRelease(mustApp(), releaseID, nil); err != nil {
		return err
	}

	log.Printf("Successfully rolled back to release %s.\n", releaseID)

	return nil
}
