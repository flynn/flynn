package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	cfg "github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
)

func init() {
	register("docker", runDocker, `
usage: flynn docker login
       flynn docker logout
       flynn docker push <image>

Deploy Docker images to a Flynn cluster.

Commands:
	login    run "docker login" against the cluster's docker-receive app

	logout   run "docker logout" against the cluster's docker-receive app

	push     push and release a Docker image to the cluster

Example:

	Assuming you have a Docker image tagged "my-custom-image:v2":

	$ flynn docker push my-custom-image:v2
	flynn: getting image config with "docker inspect -f {{ json .Config }} my-custom-image:v2"
	flynn: tagging Docker image with "docker tag --force my-custom-image:v2 docker.1.localflynn.com/my-app:latest"
	flynn: pushing Docker image with "docker push docker.1.localflynn.com/my-app:latest"
	The push refers to a repository [docker.1.localflynn.com/my-app] (len: 1)
	a8eb754d1a89: Pushed
	...
	3059b4820522: Pushed
	latest: digest: sha256:1752ca12bbedb99734ca1ba3ec35720768a95ad83b7b6c371fc37a28b98ea351 size: 61216
	flynn: image pushed, waiting for artifact creation
	flynn: deploying release using artifact URI http://docker-receive.discoverd?name=my-app&id=sha256:1752ca12bbedb99734ca1ba3ec35720768a95ad83b7b6c371fc37a28b98ea351
	flynn: image deployed, scale it with 'flynn scale app=N'
`)
}

func runDocker(args *docopt.Args, client controller.Client) error {
	if args.Bool["login"] {
		return runDockerLogin()
	} else if args.Bool["logout"] {
		return runDockerLogout()
	} else if args.Bool["push"] {
		return runDockerPush(args, client)
	}
	return errors.New("unknown docker subcommand")
}

func runDockerLogin() error {
	cluster, err := getCluster()
	if err != nil {
		return err
	}
	host, err := cluster.DockerHost()
	if err != nil {
		return err
	}
	err = dockerLogin(host, cluster.Key)
	if e, ok := err.(*exec.Error); ok && e.Err == exec.ErrNotFound {
		err = errors.New("Executable 'docker' was not found.")
	} else if err == ErrDockerTLSError {
		printDockerTLSWarning(host, cfg.CACertPath(cluster.Name))
		err = errors.New("Error configuring docker, follow the above instructions and try again.")
	}
	return err
}

func runDockerLogout() error {
	cluster, err := getCluster()
	if err != nil {
		return err
	}
	host, err := cluster.DockerHost()
	if err != nil {
		return err
	}
	cmd := dockerLogoutCmd(host)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var ErrDockerTLSError = errors.New("docker TLS error")

func dockerLogin(host, key string) error {
	var out bytes.Buffer
	cmd := exec.Command("docker", "login", "--email=user@"+host, "--username=user", "--password="+key, host)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if strings.Contains(out.String(), "certificate signed by unknown authority") {
		err = ErrDockerTLSError
	}
	return err
}

func dockerLogout(host string) error {
	return dockerLogoutCmd(host).Run()
}

func dockerLogoutCmd(host string) *exec.Cmd {
	return exec.Command("docker", "logout", host)
}

func printDockerTLSWarning(host, caPath string) {
	fmt.Printf(`
WARN: docker configuration failed with a TLS error.
WARN:
WARN: Copy the TLS CA certificate %s
WARN: to /etc/docker/certs.d/%s/ca.crt
WARN: on the docker daemon's host and restart docker.

`[1:], caPath, host)
}

func runDockerPush(args *docopt.Args, client controller.Client) error {
	image := args.String["<image>"]

	prevRelease, err := client.GetAppRelease(mustApp())
	if err == controller.ErrNotFound {
		prevRelease = &ct.Release{}
	} else if err != nil {
		return fmt.Errorf("error getting current app release:", err)
	}

	// get the image config to determine Cmd, Entrypoint and Env
	cmd := exec.Command("docker", "inspect", "-f", "{{ json .Config }}", image)
	log.Printf("flynn: getting image config with %q", strings.Join(cmd.Args, " "))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	var config struct {
		Cmd        []string `json:"Cmd"`
		Entrypoint []string `json:"Entrypoint"`
		Env        []string `json:"Env"`
	}
	if err := json.NewDecoder(stdout).Decode(&config); err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		return err
	}

	// subscribe to artifact events
	events := make(chan *ct.Event)
	stream, err := client.StreamEvents(ct.StreamEventsOptions{
		ObjectTypes: []ct.EventType{ct.EventTypeArtifact},
	}, events)
	if err != nil {
		return err
	}
	defer stream.Close()

	// push the Docker image to docker-receive
	cluster, err := getCluster()
	if err != nil {
		return err
	}
	dockerHost, err := cluster.DockerHost()
	if err != nil {
		return err
	}
	tag := fmt.Sprintf("%s/%s:latest", dockerHost, mustApp())
	cmd = exec.Command("docker", "tag", "--force", image, tag)
	log.Printf("flynn: tagging Docker image with %q", strings.Join(cmd.Args, " "))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	cmd = exec.Command("docker", "push", tag)
	log.Printf("flynn: pushing Docker image with %q", strings.Join(cmd.Args, " "))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	// wait for an artifact to be created
	log.Printf("flynn: image pushed, waiting for artifact creation")
	var artifact ct.Artifact
loop:
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return fmt.Errorf("event stream closed unexpectedly: %s", stream.Err())
			}
			if err := json.Unmarshal(event.Data, &artifact); err != nil {
				return err
			}
			if artifact.Meta["docker-receive.repository"] == mustApp() {
				break loop
			}
		case <-time.After(30 * time.Second):
			return fmt.Errorf("timed out waiting for artifact creation")
		}
	}

	// create and deploy a release with the image config and created artifact
	log.Printf("flynn: deploying release using artifact URI %s", artifact.URI)
	release := &ct.Release{
		ArtifactIDs: []string{artifact.ID},
		Processes:   prevRelease.Processes,
		Env:         prevRelease.Env,
		Meta:        prevRelease.Meta,
	}

	proc, ok := release.Processes["app"]
	if !ok {
		proc = ct.ProcessType{}
	}
	proc.Cmd = config.Cmd
	proc.Entrypoint = config.Entrypoint
	if len(proc.Ports) == 0 {
		proc.Ports = []ct.Port{{
			Port:  8080,
			Proto: "tcp",
			Service: &host.Service{
				Name:   mustApp(),
				Create: true,
			},
		}}
	}
	if release.Processes == nil {
		release.Processes = make(map[string]ct.ProcessType, 1)
	}
	release.Processes["app"] = proc

	if len(config.Env) > 0 && release.Env == nil {
		release.Env = make(map[string]string, len(config.Env))
	}
	for _, v := range config.Env {
		keyVal := strings.SplitN(v, "=", 2)
		if len(keyVal) != 2 {
			continue
		}
		release.Env[keyVal[0]] = keyVal[1]
	}

	if release.Meta == nil {
		release.Meta = make(map[string]string, 1)
	}
	release.Meta["docker-receive"] = "true"

	if err := client.CreateRelease(release); err != nil {
		return err
	}
	if err := client.DeployAppRelease(mustApp(), release.ID); err != nil {
		return err
	}
	log.Printf("flynn: image deployed, scale it with 'flynn scale app=N'")
	return nil
}
