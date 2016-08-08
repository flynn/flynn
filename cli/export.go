package main

import (
	"archive/tar"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/cheggaaa/pb"
	"github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/backup"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/router/types"
	"github.com/flynn/go-docopt"
)

func init() {
	register("export", runExport, `
usage: flynn export [options]

Export application configuration and data.

The application's metadata, deploy strategy, release configuration, slug,
formation, and Postgres database will be exported to a tar file.

Options:
	-f, --file=<file>  name of file to export to (defaults to stdout)
	-q, --quiet        don't print progress
`)

	register("import", runImport, `
usage: flynn import [options]

Create a new application using exported configuration and data.

The application will be created using the metadata, deploy strategy, release
configuration, slug, formation, and Postgres database from the provided export
file.

Options:
	-f, --file=<file>  name of file to import from (defaults to stdin)
	-n, --name=<name>  name of app to create (defaults to exported app name)
	-q, --quiet        don't print progress
	-r, --routes       import routes
`)
}

func runExport(args *docopt.Args, client controller.Client) error {
	var dest io.Writer = os.Stdout
	if filename := args.String["--file"]; filename != "" {
		f, err := os.Create(filename)
		if err != nil {
			return fmt.Errorf("error creating export file: %s", err)
		}
		defer f.Close()
		dest = f
	}

	app, err := client.GetApp(mustApp())
	if err != nil {
		return fmt.Errorf("error getting app: %s", err)
	}

	var bar backup.ProgressBar
	if !args.Bool["--quiet"] && term.IsTerminal(os.Stderr.Fd()) {
		b := pb.New(0)
		b.SetUnits(pb.U_BYTES)
		b.ShowBar = false
		b.ShowSpeed = true
		b.Output = os.Stderr
		b.Start()
		defer b.Finish()
		bar = b
	}

	tw := backup.NewTarWriter(app.Name, dest, bar)
	defer tw.Close()

	if err := tw.WriteJSON("app.json", app); err != nil {
		return fmt.Errorf("error exporting app: %s", err)
	}

	routes, err := client.RouteList(mustApp())
	if err != nil {
		return fmt.Errorf("error getting routes: %s", err)
	}
	if err := tw.WriteJSON("routes.json", routes); err != nil {
		return fmt.Errorf("error exporting routes: %s", err)
	}

	release, err := client.GetAppRelease(mustApp())
	if err == controller.ErrNotFound {
		// if the app has no release then there is nothing more to export
		return nil
	} else if err != nil {
		return fmt.Errorf("error retrieving app: %s", err)
	} else if err == nil {
		// Do not allow the exporting of passwords.
		delete(release.Env, "REDIS_PASSWORD")

		if err := tw.WriteJSON("release.json", release); err != nil {
			return fmt.Errorf("error exporting release: %s", err)
		}
	}

	var artifact *ct.Artifact
	if artifactID := release.ImageArtifactID(); artifactID != "" {
		artifact, err = client.GetArtifact(artifactID)
		if err != nil && err != controller.ErrNotFound {
			return fmt.Errorf("error retrieving artifact: %s", err)
		} else if err == nil {
			if err := tw.WriteJSON("artifact.json", artifact); err != nil {
				return fmt.Errorf("error exporting artifact: %s", err)
			}
		}
	}

	formation, err := client.GetFormation(mustApp(), release.ID)
	if err != nil && err != controller.ErrNotFound {
		return fmt.Errorf("error retrieving formation: %s", err)
	} else if err == nil {
		if err := tw.WriteJSON("formation.json", formation); err != nil {
			return fmt.Errorf("error exporting formation: %s", err)
		}
	}

	// if the release was deployed via docker-receive, pull the docker
	// image and add it to the export using "docker save"
	if release.IsDockerReceiveDeploy() && artifact != nil {
		cluster, err := getCluster()
		if err != nil {
			return err
		}
		host, err := cluster.DockerPushHost()
		if err != nil {
			return err
		}

		// the artifact will have an internal discoverd URL which will
		// not work if the Docker daemon is outside the cluster, so
		// generate a reference using the configured DockerPushURL
		repo := artifact.Meta["docker-receive.repository"]
		digest := artifact.Meta["docker-receive.digest"]
		ref := fmt.Sprintf("%s/%s@%s", host, repo, digest)

		// pull the Docker image
		cmd := exec.Command("docker", "pull", ref)
		log.Printf("flynn: pulling Docker image with %q", strings.Join(cmd.Args, " "))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}

		// give the image an explicit, random tag so that "docker save"
		// will export an image that we can reference on import (just
		// using the digest is not enough as "docker inspect" only
		// works with tags)
		tag := fmt.Sprintf("%s:flynn-export-%s", repo, random.String(8))
		if out, err := exec.Command("docker", "tag", "--force", ref, tag).CombinedOutput(); err != nil {
			return fmt.Errorf("error tagging docker image: %s: %q", err, out)
		}
		defer exec.Command("docker", "rmi", tag).Run()

		if err := dockerSave(tag, tw, bar); err != nil {
			return fmt.Errorf("error exporting docker image: %s", err)
		}

		// add the tag to the backup so we know how to reference the
		// image once it has been imported
		config := struct {
			Tag string `json:"tag"`
		}{tag}
		if err := tw.WriteJSON("docker-image.json", &config); err != nil {
			return fmt.Errorf("error exporting docker image: %s", err)
		}
	}

	// expect releases deployed via git to have a slug as their first file
	// artifact, and legacy releases to have SLUG_URL set
	var slugURL string
	if release.IsGitDeploy() && len(release.FileArtifactIDs()) > 0 {
		slugArtifact, err := client.GetArtifact(release.FileArtifactIDs()[0])
		if err != nil && err != controller.ErrNotFound {
			return fmt.Errorf("error retrieving slug artifact: %s", err)
		} else if err == nil {
			slugURL = slugArtifact.URI
		}
	} else if u, ok := release.Env["SLUG_URL"]; ok {
		slugURL = u
	}
	if slugURL != "" {
		reqR, reqW := io.Pipe()
		config := runConfig{
			App:        mustApp(),
			Release:    release.ID,
			DisableLog: true,
			Args:       []string{"curl", "--include", "--location", "--raw", slugURL},
			Stdout:     reqW,
			Stderr:     ioutil.Discard,
		}
		if bar != nil {
			config.Stdout = io.MultiWriter(config.Stdout, bar)
		}
		go func() {
			if err := runJob(client, config); err != nil {
				shutdown.Fatalf("error retrieving slug: %s", err)
			}
		}()
		req := bufio.NewReader(reqR)
		var res *http.Response
		maxRedirects := 5
		for i := 0; i < maxRedirects; i++ {
			res, err = http.ReadResponse(req, nil)
			if err != nil {
				return fmt.Errorf("error reading slug response: %s", err)
			}
			if res.StatusCode != http.StatusFound {
				break
			}
		}
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status getting slug: %d", res.StatusCode)
		}
		length, err := strconv.Atoi(res.Header.Get("Content-Length"))
		if err != nil {
			return fmt.Errorf("slug has missing or malformed Content-Length")
		}

		if err := tw.WriteHeader("slug.tar.gz", length); err != nil {
			return fmt.Errorf("error writing slug header: %s", err)
		}
		if _, err := io.Copy(tw, res.Body); err != nil {
			return fmt.Errorf("error writing slug: %s", err)
		}
		res.Body.Close()
	}

	if pgConfig, err := getAppPgRunConfig(client); err == nil {
		configPgDump(pgConfig)
		if err := tw.WriteCommandOutput(client, "postgres.dump", pgConfig.App, &ct.NewJob{
			ReleaseID:  pgConfig.Release,
			Args:       pgConfig.Args,
			Env:        pgConfig.Env,
			DisableLog: pgConfig.DisableLog,
		}); err != nil {
			return fmt.Errorf("error creating postgres dump: %s", err)
		}
	}

	if mysqlConfig, err := getAppMysqlRunConfig(client); err == nil {
		configMysqlDump(mysqlConfig)
		if err := tw.WriteCommandOutput(client, "mysql.dump", mysqlConfig.App, &ct.NewJob{
			ReleaseID:  mysqlConfig.Release,
			Args:       mysqlConfig.Args,
			Env:        mysqlConfig.Env,
			DisableLog: mysqlConfig.DisableLog,
		}); err != nil {
			return fmt.Errorf("error creating mysql dump: %s", err)
		}
	}

	return nil
}

func runImport(args *docopt.Args, client controller.Client) error {
	var src io.Reader = os.Stdin
	if filename := args.String["--file"]; filename != "" {
		f, err := os.Open(filename)
		if err != nil {
			return fmt.Errorf("error opening export file: %s", err)
		}
		defer f.Close()
		src = f
	}
	tr := tar.NewReader(src)

	var (
		app           *ct.App
		release       *ct.Release
		imageArtifact *ct.Artifact
		formation     *ct.Formation
		routes        []router.Route
		slug          io.Reader
		dockerImage   struct {
			config struct {
				Tag string `json:"tag"`
			}
			archive io.Reader
		}
		pgDump     io.Reader
		mysqlDump  io.Reader
		uploadSize int64
	)
	numResources := 0
	numRoutes := 1

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("error reading export tar: %s", err)
		}

		switch path.Base(header.Name) {
		case "app.json":
			app = &ct.App{}
			if err := json.NewDecoder(tr).Decode(app); err != nil {
				return fmt.Errorf("error decoding app: %s", err)
			}
			app.ID = ""
		case "release.json":
			release = &ct.Release{}
			if err := json.NewDecoder(tr).Decode(release); err != nil {
				return fmt.Errorf("error decoding release: %s", err)
			}
			release.ID = ""
			release.ArtifactIDs = nil
		case "artifact.json":
			imageArtifact = &ct.Artifact{}
			if err := json.NewDecoder(tr).Decode(imageArtifact); err != nil {
				return fmt.Errorf("error decoding image artifact: %s", err)
			}
			imageArtifact.ID = ""
		case "formation.json":
			formation = &ct.Formation{}
			if err := json.NewDecoder(tr).Decode(formation); err != nil {
				return fmt.Errorf("error decoding formation: %s", err)
			}
			formation.AppID = ""
			formation.ReleaseID = ""
		case "routes.json":
			if err := json.NewDecoder(tr).Decode(&routes); err != nil {
				return fmt.Errorf("error decoding routes: %s", err)
			}
			for _, route := range routes {
				route.ID = ""
				route.ParentRef = ""
			}
		case "slug.tar.gz":
			f, err := ioutil.TempFile("", "slug.tar.gz")
			if err != nil {
				return fmt.Errorf("error creating slug tempfile: %s", err)
			}
			defer f.Close()
			defer os.Remove(f.Name())
			if _, err := io.Copy(f, tr); err != nil {
				return fmt.Errorf("error reading slug: %s", err)
			}
			if _, err := f.Seek(0, os.SEEK_SET); err != nil {
				return fmt.Errorf("error seeking slug tempfile: %s", err)
			}
			slug = f
			uploadSize += header.Size
		case "docker-image.json":
			if err := json.NewDecoder(tr).Decode(&dockerImage.config); err != nil {
				return fmt.Errorf("error decoding docker image json: %s", err)
			}
		case "docker-image.tar":
			f, err := ioutil.TempFile("", "docker-image.tar")
			if err != nil {
				return fmt.Errorf("error creating docker image tempfile: %s", err)
			}
			defer f.Close()
			defer os.Remove(f.Name())
			if _, err := io.Copy(f, tr); err != nil {
				return fmt.Errorf("error reading docker image: %s", err)
			}
			if _, err := f.Seek(0, os.SEEK_SET); err != nil {
				return fmt.Errorf("error seeking docker image tempfile: %s", err)
			}
			dockerImage.archive = f
			uploadSize += header.Size
		case "postgres.dump":
			f, err := ioutil.TempFile("", "postgres.dump")
			if err != nil {
				return fmt.Errorf("error creating db tempfile: %s", err)
			}
			defer f.Close()
			defer os.Remove(f.Name())
			if _, err := io.Copy(f, tr); err != nil {
				return fmt.Errorf("error reading db dump: %s", err)
			}
			if _, err := f.Seek(0, os.SEEK_SET); err != nil {
				return fmt.Errorf("error seeking db tempfile: %s", err)
			}
			pgDump = f
			uploadSize += header.Size
		case "mysql.dump":
			f, err := ioutil.TempFile("", "mysql.dump")
			if err != nil {
				return fmt.Errorf("error creating db tempfile: %s", err)
			}
			defer f.Close()
			defer os.Remove(f.Name())
			if _, err := io.Copy(f, tr); err != nil {
				return fmt.Errorf("error reading db dump: %s", err)
			}
			if _, err := f.Seek(0, os.SEEK_SET); err != nil {
				return fmt.Errorf("error seeking db tempfile: %s", err)
			}
			mysqlDump = f
			uploadSize += header.Size
		}
	}

	if app == nil {
		return fmt.Errorf("missing app.json")
	}
	oldName := app.Name
	if name := args.String["--name"]; name != "" {
		app.Name = name
	}
	if err := client.CreateApp(app); err != nil {
		return fmt.Errorf("error creating app: %s", err)
	}

	var bar *pb.ProgressBar
	if !args.Bool["--quiet"] && uploadSize > 0 && term.IsTerminal(os.Stderr.Fd()) {
		bar = pb.New(0)
		bar.SetUnits(pb.U_BYTES)
		bar.Total = uploadSize
		bar.ShowSpeed = true
		bar.Output = os.Stderr
		bar.Start()
		defer bar.Finish()
	}

	if pgDump != nil && release != nil {
		res, err := client.ProvisionResource(&ct.ResourceReq{
			ProviderID: "postgres",
			Apps:       []string{app.ID},
		})
		if err != nil {
			return fmt.Errorf("error provisioning postgres resource: %s", err)
		}
		numResources++

		if release.Env == nil {
			release.Env = make(map[string]string, len(res.Env))
		}
		for k, v := range res.Env {
			release.Env[k] = v
		}

		config, err := getPgRunConfig(client, app.ID, release)
		if err != nil {
			return fmt.Errorf("error getting postgres config: %s", err)
		}
		config.Stdin = pgDump
		if bar != nil {
			config.Stdin = bar.NewProxyReader(config.Stdin)
		}
		config.Exit = false
		if err := pgRestore(client, config); err != nil {
			return fmt.Errorf("error restoring postgres database: %s", err)
		}
	}

	if mysqlDump != nil && release != nil {
		res, err := client.ProvisionResource(&ct.ResourceReq{
			ProviderID: "mysql",
			Apps:       []string{app.ID},
		})
		if err != nil {
			return fmt.Errorf("error provisioning mysql resource: %s", err)
		}
		numResources++

		if release.Env == nil {
			release.Env = make(map[string]string, len(res.Env))
		}
		for k, v := range res.Env {
			release.Env[k] = v
		}

		config, err := getMysqlRunConfig(client, app.ID, release)
		if err != nil {
			return fmt.Errorf("error getting mysql config: %s", err)
		}
		config.Stdin = mysqlDump
		if bar != nil {
			config.Stdin = bar.NewProxyReader(config.Stdin)
		}
		config.Exit = false
		if err := mysqlRestore(client, config); err != nil {
			return fmt.Errorf("error restoring mysql database: %s", err)
		}
	}

	if release != nil && release.Env["FLYNN_REDIS"] != "" {
		res, err := client.ProvisionResource(&ct.ResourceReq{
			ProviderID: "redis",
			Apps:       []string{app.ID},
		})
		if err != nil {
			return fmt.Errorf("error provisioning redis resource: %s", err)
		}
		numResources++

		if release.Env == nil {
			release.Env = make(map[string]string, len(res.Env))
		}
		for k, v := range res.Env {
			release.Env[k] = v
		}
	}

	uploadSlug := release != nil && imageArtifact != nil && slug != nil

	if uploadSlug {
		// Use current slugrunner as the artifact
		gitreceiveRelease, err := client.GetAppRelease("gitreceive")
		if err != nil {
			return fmt.Errorf("unable to retrieve gitreceive release: %s", err)
		}
		if id, ok := gitreceiveRelease.Env["SLUGRUNNER_IMAGE_ID"]; ok {
			imageArtifact, err = client.GetArtifact(id)
			if err != nil {
				return fmt.Errorf("unable to get slugrunner image artifact: %s", err)
			}
		} else if uri, ok := gitreceiveRelease.Env["SLUGRUNNER_IMAGE_URI"]; ok {
			imageArtifact = &ct.Artifact{
				Type: host.ArtifactTypeDocker,
				URI:  uri,
			}
		} else {
			return fmt.Errorf("gitreceive env missing slug runner image")
		}
	}

	if dockerImage.config.Tag != "" && dockerImage.archive != nil {
		// load the docker image into the Docker daemon
		cmd := exec.Command("docker", "load")
		cmd.Stdin = dockerImage.archive
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("error running docker load: %s: %q", err, out)
		}

		// use the tag from the config (which will now be applied to
		// the loaded image) to push the image to docker-receive
		cluster, err := getCluster()
		if err != nil {
			return err
		}
		host, err := cluster.DockerPushHost()
		if err != nil {
			return err
		}
		tag := fmt.Sprintf("%s/%s:latest", host, app.Name)
		if out, err := exec.Command("docker", "tag", "--force", dockerImage.config.Tag, tag).CombinedOutput(); err != nil {
			return fmt.Errorf("error tagging docker image: %s: %q", err, out)
		}

		artifact, err := dockerPush(client, app.Name, tag)
		if err != nil {
			return fmt.Errorf("error pushing docker image: %s", err)
		}

		release.ArtifactIDs = []string{artifact.ID}
	} else if imageArtifact != nil {
		if imageArtifact.ID == "" {
			if err := client.CreateArtifact(imageArtifact); err != nil {
				return fmt.Errorf("error creating image artifact: %s", err)
			}
		}
		release.ArtifactIDs = []string{imageArtifact.ID}
	}

	if release != nil {
		for t, proc := range release.Processes {
			for i, port := range proc.Ports {
				if port.Service != nil && strings.HasPrefix(port.Service.Name, oldName) {
					proc.Ports[i].Service.Name = strings.Replace(port.Service.Name, oldName, app.Name, 1)
				}
			}
			release.Processes[t] = proc
		}
		if err := client.CreateRelease(release); err != nil {
			return fmt.Errorf("error creating release: %s", err)
		}
		if err := client.SetAppRelease(app.ID, release.ID); err != nil {
			return fmt.Errorf("error setting app release: %s", err)
		}
	}

	if uploadSlug {
		slugURI := fmt.Sprintf("http://blobstore.discoverd/%s/slug.tgz", random.UUID())
		config := runConfig{
			App:        app.ID,
			Release:    release.ID,
			DisableLog: true,
			Args:       []string{"curl", "--request", "PUT", "--upload-file", "-", slugURI},
			Stdin:      slug,
			Stdout:     ioutil.Discard,
			Stderr:     ioutil.Discard,
		}
		if bar != nil {
			config.Stdin = bar.NewProxyReader(config.Stdin)
		}
		if err := runJob(client, config); err != nil {
			return fmt.Errorf("error uploading slug: %s", err)
		}
		slugArtifact := &ct.Artifact{
			Type: host.ArtifactTypeFile,
			URI:  slugURI,
		}
		if err := client.CreateArtifact(slugArtifact); err != nil {
			return fmt.Errorf("error creating slug artifact: %s", err)
		}
		release.ID = ""
		release.ArtifactIDs = append(release.ArtifactIDs, slugArtifact.ID)
		if release.Meta == nil {
			release.Meta = make(map[string]string, 1)
		}
		release.Meta["git"] = "true"
		if err := client.CreateRelease(release); err != nil {
			return fmt.Errorf("error creating release: %s", err)
		}
		if err := client.SetAppRelease(app.ID, release.ID); err != nil {
			return fmt.Errorf("error setting app release: %s", err)
		}
	}

	if formation != nil && release != nil {
		formation.ReleaseID = release.ID
		formation.AppID = app.ID
		if err := client.PutFormation(formation); err != nil {
			return fmt.Errorf("error creating formation: %s", err)
		}
	}

	if args.Bool["--routes"] {
		for _, route := range routes {
			if err := client.CreateRoute(app.ID, &route); err != nil {
				if e, ok := err.(hh.JSONError); ok && e.Code == hh.ConflictErrorCode {
					// If the cluster domain matches then the default route
					// exported will conflict with the one created automatically.
					continue
				}
				return fmt.Errorf("error creating route: %s", err)
			}
			numRoutes++
		}
	}

	fmt.Printf("Imported %s (added %d routes, provisioned %d resources)\n", app.Name, numRoutes, numResources)

	return nil
}
