package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/version"
	"github.com/flynn/flynn/updater/types"
	"github.com/mattn/go-colorable"
	"gopkg.in/inconshreveable/log15.v2"
)

var redisImage, slugBuilder, slugRunner *ct.Artifact

// use a flag to determine whether to use a TTY log formatter because actually
// assigning a TTY to the job causes reading images via stdin to fail.
var isTTY = flag.Bool("tty", false, "use a TTY log formatter")

const deployTimeout = 30 * time.Minute

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

	log.Info("creating new image artifacts")
	redisImage = &ct.Artifact{
		Type: host.ArtifactTypeDocker,
		URI:  uris["redis"],
	}
	if err := client.CreateArtifact(redisImage); err != nil {
		log.Error("error creating redis image artifact", "err", err)
		return err
	}
	slugRunner = &ct.Artifact{
		Type: host.ArtifactTypeDocker,
		URI:  uris["slugrunner"],
	}
	if err := client.CreateArtifact(slugRunner); err != nil {
		log.Error("error creating slugrunner image artifact", "err", err)
		return err
	}
	slugBuilder = &ct.Artifact{
		Type: host.ArtifactTypeDocker,
		URI:  uris["slugbuilder"],
	}
	if err := client.CreateArtifact(slugBuilder); err != nil {
		log.Error("error creating slugbuilder image artifact", "err", err)
		return err
	}

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
		if err == controller.ErrNotFound && appInfo.Optional {
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

	// deploy all other apps (including provisioned Redis apps)
	apps, err := client.AppList()
	if err != nil {
		log.Error("error getting apps", "err", err)
		return err
	}
	for _, app := range apps {
		log := log.New("name", app.Name)

		if app.RedisAppliance() {
			log.Info("starting deploy of Redis app")
			if err := deployApp(client, app, redisImage.URI, nil, log); err != nil {
				if e, ok := err.(errDeploySkipped); ok {
					log.Info("skipped deploy of Redis app", "reason", e.reason)
					continue
				}
				return err
			}
			log.Info("finished deploy of Redis app")
			continue
		}

		if app.System() {
			continue
		}

		log.Info("starting deploy of app to update slugrunner")
		if err := deployApp(client, app, slugRunner.URI, nil, log); err != nil {
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

func deployApp(client controller.Client, app *ct.App, uri string, updateFn updater.UpdateReleaseFn, log log15.Logger) error {
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
		if !strings.HasSuffix(u.Query().Get("target"), "slugrunner.json") {
			return errDeploySkipped{"app not using slugrunner image"}
		}
	}
	skipDeploy := artifact.URI == uri
	if updateImageIDs(release.Env) {
		skipDeploy = false
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
	timeoutCh := make(chan struct{})
	time.AfterFunc(deployTimeout, func() { close(timeoutCh) })
	if err := client.DeployAppRelease(app.ID, release.ID, timeoutCh); err != nil {
		log.Error("error deploying app", "err", err)
		return err
	}
	return nil
}

// updateImageIDs updates REDIS_IMAGE_ID, SLUGBUILDER_IMAGE_ID and
// SLUGRUNNER_IMAGE_ID if they are set and have an old ID, and also
// replaces the legacy REDIS_IMAGE_URI, SLUGBUILDER_IMAGE_URI and
// SLUGRUNNER_IMAGE_URI
func updateImageIDs(env map[string]string) bool {
	updated := false
	for prefix, newID := range map[string]string{
		"REDIS":       redisImage.ID,
		"SLUGBUILDER": slugBuilder.ID,
		"SLUGRUNNER":  slugRunner.ID,
	} {
		idKey := prefix + "_IMAGE_ID"
		if id, ok := env[idKey]; ok && id != newID {
			env[idKey] = newID
			updated = true
		}

		uriKey := prefix + "_IMAGE_URI"
		if _, ok := env[uriKey]; ok {
			delete(env, uriKey)
			env[idKey] = newID
			updated = true
		}
	}
	return updated
}
