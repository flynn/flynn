package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/exec"
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
	services, err := discoverd.Services("shelf", discoverd.DefaultTimeout)
	if err != nil || len(services) < 1 {
		log.Fatalf("Unable to discover shelf %q", err)
	}
	shelfHost := services[0].Addr

	app := os.Args[1]
	commit := os.Args[2]

	_, err = client.GetApp(app)
	if err == controller.ErrNotFound {
		log.Fatalf("Unknown app %q", app)
	} else if err != nil {
		log.Fatalln("Error retrieving app:", err)
	}
	prevRelease, err := client.GetAppRelease(app)
	if err == controller.ErrNotFound {
		prevRelease = &ct.Release{}
	} else if err != nil {
		log.Fatalln("Error creating getting current app release:", err)
	}

	fmt.Printf("-----> Building %s...\n", app)

	var output bytes.Buffer
	slugURL := fmt.Sprintf("http://%s/%s.tgz", shelfHost, commit)
	cmd := exec.Command(exec.DockerImage("flynn/slugbuilder", os.Getenv("SLUGBUILDER_IMAGE_ID")), slugURL)
	cmd.Stdout = io.MultiWriter(os.Stdout, &output)
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
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

	artifact := &ct.Artifact{Type: "docker", URI: "https://registry.hub.docker.com/flynn/slugrunner?id=" + os.Getenv("SLUGRUNNER_IMAGE_ID")}
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
			proc.Env["SD_NAME"] = app + "-web"
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
	if err := client.SetAppRelease(app, release.ID); err != nil {
		log.Fatalln("Error setting app release:", err)
	}

	fmt.Println("=====> Application deployed")
}
