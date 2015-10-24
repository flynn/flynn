package main

import (
	"archive/tar"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strconv"
	"syscall"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cheggaaa/pb"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
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
`)
}

func runExport(args *docopt.Args, client *controller.Client) error {
	var dest io.Writer = os.Stdout
	if filename := args.String["--file"]; filename != "" {
		f, err := os.Create(filename)
		if err != nil {
			return fmt.Errorf("error creating export file: %s", err)
		}
		defer f.Close()
		dest = f
	}
	tw := tar.NewWriter(dest)
	defer tw.Close()

	uid := syscall.Getuid()
	header := func(name string, length int) error {
		return tw.WriteHeader(&tar.Header{
			Name:     path.Join(mustApp(), name),
			Mode:     0644,
			Size:     int64(length),
			ModTime:  time.Now(),
			Typeflag: tar.TypeReg,
			Uid:      uid,
			Gid:      uid,
		})
	}
	writeJSON := func(name string, v interface{}) error {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		if err := header(name, len(data)+1); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
		if _, err := tw.Write([]byte("\n")); err != nil {
			return err
		}
		return nil
	}

	app, err := client.GetApp(mustApp())
	if err != nil {
		return fmt.Errorf("error getting app: %s", err)
	}
	if err := writeJSON("app.json", app); err != nil {
		return fmt.Errorf("error exporting app: %s", err)
	}

	routes, err := client.RouteList(mustApp())
	if err != nil {
		return fmt.Errorf("error getting routes: %s", err)
	}
	if err := writeJSON("routes.json", routes); err != nil {
		return fmt.Errorf("error exporting routes: %s", err)
	}

	release, err := client.GetAppRelease(mustApp())
	if err != nil && err != controller.ErrNotFound {
		return fmt.Errorf("error retrieving app: %s", err)
	} else if err == nil {
		if err := writeJSON("release.json", release); err != nil {
			return fmt.Errorf("error exporting release: %s", err)
		}
	}

	artifact, err := client.GetArtifact(release.ArtifactID)
	if err != nil && err != controller.ErrNotFound {
		return fmt.Errorf("error retrieving artifact: %s", err)
	} else if err == nil {
		if err := writeJSON("artifact.json", artifact); err != nil {
			return fmt.Errorf("error exporting artifact: %s", err)
		}
	}

	formation, err := client.GetFormation(mustApp(), release.ID)
	if err != nil && err != controller.ErrNotFound {
		return fmt.Errorf("error retrieving formation: %s", err)
	} else if err == nil {
		if err := writeJSON("formation.json", formation); err != nil {
			return fmt.Errorf("error exporting formation: %s", err)
		}
	}

	var bar *pb.ProgressBar
	if !args.Bool["--quiet"] && term.IsTerminal(os.Stderr.Fd()) {
		bar = pb.New(0)
		bar.SetUnits(pb.U_BYTES)
		bar.ShowBar = false
		bar.ShowSpeed = true
		bar.Output = os.Stderr
		bar.Start()
		defer bar.Finish()
	}

	if slug, ok := release.Env["SLUG_URL"]; ok {
		reqR, reqW := io.Pipe()
		config := runConfig{
			App:        mustApp(),
			Release:    release.ID,
			DisableLog: true,
			Entrypoint: []string{"curl"},
			Args:       []string{"--include", "--raw", slug},
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
		res, err := http.ReadResponse(bufio.NewReader(reqR), nil)
		if err != nil {
			return fmt.Errorf("error reading slug response: %s", err)
		}
		if res.StatusCode != 200 {
			return fmt.Errorf("unexpected status getting slug: %d", res.StatusCode)
		}
		length, err := strconv.Atoi(res.Header.Get("Content-Length"))
		if err != nil {
			return fmt.Errorf("slug has missing or malformed Content-Length")
		}

		if err := header("slug.tar.gz", length); err != nil {
			return fmt.Errorf("error writing slug header: %s", err)
		}
		if _, err := io.Copy(tw, res.Body); err != nil {
			return fmt.Errorf("error writing slug: %s", err)
		}
		res.Body.Close()
	}

	if config, err := getAppPgRunConfig(client); err == nil {
		f, err := ioutil.TempFile("", "postgres.dump")
		if err != nil {
			return fmt.Errorf("error creating db temp file: %s", err)
		}
		defer f.Close()
		defer os.Remove(f.Name())
		config.Stdout = f
		config.Exit = false

		if bar != nil {
			config.Stdout = io.MultiWriter(config.Stdout, bar)
		}
		if err := pgDump(client, config); err != nil {
			return fmt.Errorf("error dumping database: %s", err)
		}

		length, err := f.Seek(0, os.SEEK_CUR)
		if err != nil {
			return fmt.Errorf("error getting db size: %s", err)
		}
		if err := header("postgres.dump", int(length)); err != nil {
			return fmt.Errorf("error writing db header: %s", err)
		}
		if _, err := f.Seek(0, os.SEEK_SET); err != nil {
			return fmt.Errorf("error seeking db dump: %s", err)
		}
		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("error exporting db: %s", err)
		}
	}

	return nil
}

func runImport(args *docopt.Args, client *controller.Client) error {
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
		app        *ct.App
		release    *ct.Release
		artifact   *ct.Artifact
		formation  *ct.Formation
		slug       io.Reader
		pgDump     io.Reader
		uploadSize int64
	)

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
			release.ArtifactID = ""
		case "artifact.json":
			artifact = &ct.Artifact{}
			if err := json.NewDecoder(tr).Decode(artifact); err != nil {
				return fmt.Errorf("error decoding artifact: %s", err)
			}
			artifact.ID = ""
		case "formation.json":
			formation = &ct.Formation{}
			if err := json.NewDecoder(tr).Decode(formation); err != nil {
				return fmt.Errorf("error decoding formation: %s", err)
			}
			formation.AppID = ""
			formation.ReleaseID = ""
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

	uploadSlug := release != nil && release.Env["SLUG_URL"] != "" && artifact != nil && slug != nil

	if uploadSlug {
		// Use current slugrunner as the artifact
		gitreceiveRelease, err := client.GetAppRelease("gitreceive")
		if err != nil {
			return fmt.Errorf("unable to retrieve gitreceive release: %s", err)
		}
		artifact = &ct.Artifact{
			Type: "docker",
			URI:  gitreceiveRelease.Env["SLUGRUNNER_IMAGE_URI"],
		}
		if artifact.URI == "" {
			return fmt.Errorf("gitreceive env missing SLUGRUNNER_IMAGE_URI")
		}
		release.Env["SLUG_URL"] = fmt.Sprintf("http://blobstore.discoverd/%s.tgz", random.UUID())
	}

	if artifact != nil {
		if err := client.CreateArtifact(artifact); err != nil {
			return fmt.Errorf("error creating artifact: %s", err)
		}
		release.ArtifactID = artifact.ID
	}

	if release != nil {
		for t, proc := range release.Processes {
			for i, port := range proc.Ports {
				if port.Service != nil && port.Service.Name == oldName+"-web" {
					proc.Ports[i].Service.Name = app.Name + "-web"
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
		config := runConfig{
			App:        app.ID,
			Release:    release.ID,
			DisableLog: true,
			Entrypoint: []string{"curl"},
			Args:       []string{"--request", "PUT", "--upload-file", "-", release.Env["SLUG_URL"]},
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
	}

	if formation != nil && release != nil {
		formation.ReleaseID = release.ID
		formation.AppID = app.ID
		if err := client.PutFormation(formation); err != nil {
			return fmt.Errorf("error creating formation: %s", err)
		}
	}

	return nil
}
