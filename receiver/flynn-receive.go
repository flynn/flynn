package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/random"
)

var clusterc *cluster.Client

func init() {
	log.SetFlags(0)

	var err error
	clusterc, err = cluster.NewClient()
	if err != nil {
		log.Fatalln("Error connecting to cluster leader:", err)
	}
}

var typesPattern = regexp.MustCompile("types.* -> (.+)\n")

func main() {
	client, err := controller.NewClient("", os.Getenv("CONTROLLER_AUTH_KEY"))
	if err != nil {
		log.Fatalln("Unable to connect to controller:", err)
	}
	// TODO: use discoverd http dialer here?
	services, err := discoverd.Services("blobstore", discoverd.DefaultTimeout)
	if err != nil || len(services) < 1 {
		log.Fatalf("Unable to discover blobstore %q", err)
	}
	blobstoreHost := services[0].Addr

	appName := os.Args[1]

	app, err := client.GetApp(appName)
	if err == controller.ErrNotFound {
		log.Fatalf("Unknown app %q", appName)
	} else if err != nil {
		log.Fatalln("Error retrieving app:", err)
	}
	prevRelease, err := client.GetAppRelease(app.Name)
	if err == controller.ErrNotFound {
		prevRelease = &ct.Release{}
	} else if err != nil {
		log.Fatalln("Error getting current app release:", err)
	}

	fmt.Printf("-----> Building %s...\n", app.Name)

	var output bytes.Buffer
	slugURL := fmt.Sprintf("http://%s/%s.tgz", blobstoreHost, random.UUID())
	cmd := exec.Command(exec.DockerImage(os.Getenv("SLUGBUILDER_IMAGE_URI")), slugURL)
	cmd.Stdout = io.MultiWriter(os.Stdout, &output)
	cmd.Stderr = os.Stderr
	if len(prevRelease.Env) > 0 {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			log.Fatalln(err)
		}
		go appendEnvDir(os.Stdin, stdin, prevRelease.Env)
	} else {
		cmd.Stdin = os.Stdin
	}
	if buildpackURL, ok := prevRelease.Env["BUILDPACK_URL"]; ok {
		cmd.Env = map[string]string{"BUILDPACK_URL": buildpackURL}
	}

	if err := cmd.Run(); err != nil {
		log.Fatalln("Build failed:", err)
	}

	var types []string
	if match := typesPattern.FindSubmatch(output.Bytes()); match != nil {
		types = strings.Split(string(match[1]), ", ")
	}

	fmt.Printf("-----> Creating release...\n")

	artifact := &ct.Artifact{Type: "docker", URI: os.Getenv("SLUGRUNNER_IMAGE_URI")}
	if err := client.CreateArtifact(artifact); err != nil {
		log.Fatalln("Error creating artifact:", err)
	}

	release := &ct.Release{
		ArtifactID: artifact.ID,
		Env:        prevRelease.Env,
	}
	procs := make(map[string]ct.ProcessType)
	for _, t := range types {
		proc := prevRelease.Processes[t]
		proc.Cmd = []string{"start", t}
		if t == "web" {
			proc.Ports = []ct.Port{{Proto: "tcp"}}
			if proc.Env == nil {
				proc.Env = make(map[string]string)
			}
			proc.Env["SD_NAME"] = app.Name + "-web"
		}
		procs[t] = proc
	}
	release.Processes = procs
	if release.Env == nil {
		release.Env = make(map[string]string)
	}
	release.Env["SLUG_URL"] = slugURL

	if err := client.CreateRelease(release); err != nil {
		log.Fatalln("Error creating release:", err)
	}
	if err := client.SetAppRelease(app.Name, release.ID); err != nil {
		log.Fatalln("Error setting app release:", err)
	}

	fmt.Println("=====> Application deployed")

	// If the app is new and the web process type exists,
	// it should scale to one process after the release is created.
	if _, ok := procs["web"]; ok && prevRelease.ID == "" {
		formation := &ct.Formation{
			AppID:     app.ID,
			ReleaseID: release.ID,
			Processes: map[string]int{"web": 1},
		}
		if err := client.PutFormation(formation); err != nil {
			log.Fatalln("Error putting formation:", err)
		}

		fmt.Println("=====> Added default web=1 formation")
	}
}

func appendEnvDir(stdin io.Reader, pipe io.WriteCloser, env map[string]string) {
	defer pipe.Close()
	tr := tar.NewReader(stdin)
	tw := tar.NewWriter(pipe)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}
		if err != nil {
			log.Fatalln(err)
		}
		hdr.Name = path.Join("app", hdr.Name)
		if err := tw.WriteHeader(hdr); err != nil {
			log.Fatalln(err)
		}
		if _, err := io.Copy(tw, tr); err != nil {
			log.Fatalln(err)
		}
	}
	// append env dir
	for key, value := range env {
		hdr := &tar.Header{
			Name:    path.Join("env", key),
			Mode:    0400,
			ModTime: time.Now(),
			Size:    int64(len(value)),
		}

		if err := tw.WriteHeader(hdr); err != nil {
			log.Fatalln(err)
		}
		if _, err := tw.Write([]byte(value)); err != nil {
			log.Fatalln(err)
		}
	}
	hdr := &tar.Header{
		Name:    ".ENV_DIR_bdca46b87df0537eaefe79bb632d37709ff1df18",
		Mode:    0400,
		ModTime: time.Now(),
		Size:    0,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		log.Fatalln(err)
	}
}
