package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/mattn/go-colorable"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/version"
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

	req, err := http.NewRequest("GET", "http://status-web.discoverd", nil)
	if err != nil {
		return err
	}
	req.Header = make(http.Header)
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error("error getting cluster status", "err", err)
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		log.Error("cluster status is unhealthy", "code", res.StatusCode)
		return fmt.Errorf("cluster is unhealthy")
	}
	var statusWrapper struct {
		Data struct {
			Detail map[string]status.Status
		}
	}
	if err := json.NewDecoder(res.Body).Decode(&statusWrapper); err != nil {
		log.Error("error decoding cluster status JSON", "err", err)
		return err
	}
	statuses := statusWrapper.Data.Detail

	instances, err := discoverd.GetInstances("controller", 10*time.Second)
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
	uris := make(map[string]string, len(updater.SystemApps))
	for _, app := range updater.SystemApps {
		if v := version.Parse(statuses[app.Name].Version); !v.Dev && app.MinVersion != "" && v.Before(version.Parse(app.MinVersion)) {
			log.Info(
				"not updating image of system app, can't upgrade from running version",
				"app", app.Name,
				"version", v,
			)
			continue
		}
		if app.Image == "" {
			app.Image = "flynn/" + app.Name
		}
		uri, ok := images[app.Image]
		if !ok {
			err := fmt.Errorf("missing image: %s", app.Image)
			log.Error(err.Error())
			return err
		}
		uris[app.Name] = uri
	}
	slugbuilderURI = uris["slugbuilder"]
	slugrunnerURI = uris["slugrunner"]

	// deploy system apps in order first
	for _, appInfo := range updater.SystemApps {
		if appInfo.ImageOnly {
			continue // skip ImageOnly updates
		}
		if _, ok := uris[appInfo.Name]; !ok {
			log.Info(
				"skipped deploy of system app",
				"reason", "image not updated",
				"app", appInfo.Name,
			)
			continue
		}
		log := log.New("name", appInfo.Name)
		log.Info("starting deploy of system app")

		app, err := client.GetApp(appInfo.Name)
		if err == client.ErrNotFound && appInfo.Optional {
			log.Info(
				"skipped deploy of system app",
				"reason", "optional app not present",
				"app", appInfo.Name,
			)
			continue
		} else if err != nil {
			log.Error("error getting app", "err", err)
			return err
		}
		if err := deployApp(client, app, uris[appInfo.Name], appInfo.UpdateRelease, log); err != nil {
			if e, ok := err.(errDeploySkipped); ok {
				log.Info(
					"skipped deploy of system app",
					"reason", e.reason,
					"app", appInfo.Name,
				)
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
		if err := deployApp(client, app, slugrunnerURI, nil, log); err != nil {
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

func deployApp(client *controller.Client, app *ct.App, uri string, updateFn updater.UpdateReleaseFn, log log15.Logger) error {
	release, err := client.GetAppRelease(app.ID)
	if err != nil {
		log.Error("error getting release", "err", err)
		return err
	}
	artifact, err := client.GetArtifact(release.ImageArtifactID())
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
	if updateSlugURIs(release.Env) {
		skipDeploy = false // deploy apps that depend on slugbuilder images if updated
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
	release.SetImageArtifactID(artifact.ID)
	if updateFn != nil {
		updateFn(release)
	}
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
	uris := map[string]string{
		"SLUGBUILDER_IMAGE_URI": slugbuilderURI,
		"SLUGRUNNER_IMAGE_URI":  slugrunnerURI,
	}
	updated := false
	for key, uri := range uris {
		if v, ok := env[key]; ok && v != uri {
			env[key] = uri
			updated = true
		}
	}
	return updated
}
