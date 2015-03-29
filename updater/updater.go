package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/mattn/go-colorable"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/updater/types"
)

var slugbuilderURI, slugrunnerURI string

// use a flag to determine whether to use a TTY log formatter because actually
// assigning a TTY to the job causes reading images via stdin to fail.
var isTTY = flag.Bool("tty", false, "use a TTY log formatter")

func main() {
	flag.Parse()
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	log := log15.New()
	if *isTTY {
		log.SetHandler(log15.StreamHandler(colorable.NewColorableStdout(), log15.TerminalFormat()))
	}

	var images map[string]string
	if err := json.NewDecoder(os.Stdin).Decode(&images); err != nil {
		log.Error("error decoding images", "err", err)
		return err
	}

	instances, err := discoverd.GetInstances("flynn-controller", 10*time.Second)
	if err != nil {
		log.Error("error looking up controller in service discovery", "err", err)
		return err
	}
	client, err := controller.NewClient("", instances[0].Meta["AUTH_KEY"])
	if err != nil {
		log.Error("error creating controller client", "err", err)
		return err
	}

	log.Info("validating images")
	uris := make(map[string]string, len(updater.SystemApps)+2)
	for _, name := range append(updater.SystemApps, "slugbuilder", "slugrunner") {
		image := "flynn/" + name
		if name == "gitreceive" {
			image = "flynn/receiver"
		} else if name == "postgres" {
			image = "flynn/postgresql"
		}
		uri, ok := images[image]
		if !ok {
			err := fmt.Errorf("missing image: %s", image)
			log.Error(err.Error())
			return err
		}
		uris[name] = uri
	}
	slugbuilderURI = uris["slugbuilder"]
	slugrunnerURI = uris["slugrunner"]

	// deploy system apps in order first
	for _, name := range updater.SystemApps {
		log := log.New("name", name)
		log.Info("starting deploy of system app")

		app, err := client.GetApp(name)
		if err != nil {
			log.Error("error getting app", "err", err)
			return err
		}
		if err := deployApp(client, app, uris[name], log); err != nil {
			if e, ok := err.(errDeploySkipped); ok {
				log.Info("skipped deploy of system app", "reason", e.reason)
				continue
			}
			return err
		}
		log.Info("finished deploy of system app")
	}

	// deploy all other apps
	apps, err := client.AppList()
	if err != nil {
		log.Error("error getting apps", "err", err)
		return err
	}
	for _, app := range apps {
		if app.System() {
			continue
		}
		log := log.New("name", app.Name)
		log.Info("starting deploy of app to update slugrunner")
		if err := deployApp(client, app, slugrunnerURI, log); err != nil {
			if e, ok := err.(errDeploySkipped); ok {
				log.Info("skipped deploy of app", "reason", e.reason)
				continue
			}
			return err
		}
		log.Info("finished deploy of app")
	}
	return nil
}

type errDeploySkipped struct {
	reason string
}

func (e errDeploySkipped) Error() string {
	return e.reason
}

func deployApp(client *controller.Client, app *ct.App, uri string, log log15.Logger) error {
	release, err := client.GetAppRelease(app.ID)
	if err != nil {
		log.Error("error getting release", "err", err)
		return err
	}
	artifact, err := client.GetArtifact(release.ArtifactID)
	if err != nil {
		log.Error("error getting release artifact", "err", err)
		return err
	}
	if !app.System() {
		u, err := url.Parse(artifact.URI)
		if err != nil {
			return err
		}
		if u.Query().Get("name") != "flynn/slugrunner" {
			return errDeploySkipped{"app not using slugrunner image"}
		}
	}
	skipDeploy := artifact.URI == uri
	// deploy the gitreceive / taffy apps if builder / runner images have changed
	switch app.Name {
	case "taffy", "gitreceive":
		if updateSlugURIs(release.Env) {
			skipDeploy = false
		}
	}
	if skipDeploy {
		return errDeploySkipped{"app is already using latest images"}
	}
	artifact.ID = ""
	artifact.URI = uri
	if err := client.CreateArtifact(artifact); err != nil {
		log.Error("error creating artifact", "err", err)
		return err
	}
	release.ID = ""
	release.ArtifactID = artifact.ID
	if err := client.CreateRelease(release); err != nil {
		log.Error("error creating new release", "err", err)
		return err
	}
	if err := client.DeployAppRelease(app.ID, release.ID); err != nil {
		log.Error("error deploying app", "err", err)
		return err
	}
	return nil
}

func updateSlugURIs(env map[string]string) bool {
	updated := false
	if env["SLUGBUILDER_IMAGE_URI"] != slugbuilderURI {
		env["SLUGBUILDER_IMAGE_URI"] = slugbuilderURI
		updated = true
	}
	if env["SLUGRUNNER_IMAGE_URI"] != slugrunnerURI {
		env["SLUGRUNNER_IMAGE_URI"] = slugrunnerURI
		updated = true
	}
	return updated
}
