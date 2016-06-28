package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	cfg "github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/backup"
	"github.com/flynn/go-docopt"
)

func init() {
	register("docker", runDocker, `
usage: flynn docker set-push-url [<url>]
       flynn docker login
       flynn docker logout
       flynn docker push <image>

Deploy Docker images to a Flynn cluster.

Commands:
	set-push-url  set the Docker push URL (defaults to https://docker.$CLUSTER_DOMAIN)

	login         run "docker login" against the cluster's docker-receive app

	logout        run "docker logout" against the cluster's docker-receive app

	push          push and release a Docker image to the cluster

Example:

	Assuming you have a Docker image tagged "my-custom-image:v2":

	$ flynn docker push my-custom-image:v2
	flynn: getting image config with "docker inspect -f {{ json .Config }} my-custom-image:v2"
	flynn: tagging Docker image with "docker tag my-custom-image:v2 docker.1.localflynn.com/my-app:latest"
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
	if args.Bool["set-push-url"] {
		return runDockerSetPushURL(args)
	} else if args.Bool["login"] {
		return runDockerLogin()
	} else if args.Bool["logout"] {
		return runDockerLogout()
	} else if args.Bool["push"] {
		return runDockerPush(args, client)
	}
	return errors.New("unknown docker subcommand")
}

func runDockerSetPushURL(args *docopt.Args) error {
	cluster, err := getCluster()
	if err != nil {
		return err
	}
	url := args.String["<url>"]
	if url == "" {
		if cluster.DockerPushURL != "" {
			return fmt.Errorf("ERROR: refusing to overwrite current Docker push URL %q with a default one. To overwrite the existing URL, set one explicitly with 'flynn docker set-push-url URL'", cluster.DockerPushURL)
		}
		if !strings.Contains(cluster.ControllerURL, "controller") {
			return errors.New("ERROR: unable to determine default Docker push URL, set one explicitly with 'flynn docker set-push-url URL'")
		}
		url = strings.Replace(cluster.ControllerURL, "controller", "docker", 1)
	}
	if !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}
	cluster.DockerPushURL = url
	return config.SaveTo(configPath())
}

func runDockerLogin() error {
	cluster, err := getCluster()
	if err != nil {
		return err
	}
	host, err := cluster.DockerPushHost()
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
	host, err := cluster.DockerPushHost()
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
		return ErrDockerTLSError
	} else if err != nil {
		return fmt.Errorf("error running `docker login`: %s - output: %q", err, out)
	}
	return nil
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
WARN:
WARN: If using Docker for Mac, go to Docker -> Preferences
WARN: -> Advanced, add %q as an
WARN: Insecure Registry and hit "Apply & Restart".

`[1:], caPath, host, host)
}

func runDockerPush(args *docopt.Args, client controller.Client) error {
	cluster, err := getCluster()
	if err != nil {
		return err
	}
	dockerHost, err := cluster.DockerPushHost()
	if err != nil {
		return err
	}

	image := args.String["<image>"]

	app, err := client.GetApp(mustApp())
	if err != nil {
		return err
	}
	prevRelease, err := client.GetAppRelease(app.ID)
	if err == controller.ErrNotFound {
		prevRelease = &ct.Release{}
	} else if err != nil {
		return fmt.Errorf("error getting current app release: %s", err)
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

	// tag the docker image ready to be pushed
	tag := fmt.Sprintf("%s/%s:latest", dockerHost, app.Name)
	cmd = exec.Command("docker", "tag", image, tag)
	log.Printf("flynn: tagging Docker image with %q", strings.Join(cmd.Args, " "))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	artifact, err := dockerPush(client, app.Name, tag)
	if err != nil {
		return err
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
	proc.Args = append(config.Entrypoint, config.Cmd...)
	if len(proc.Ports) == 0 {
		proc.Ports = []ct.Port{{
			Port:  8080,
			Proto: "tcp",
			Service: &host.Service{
				Name:   app.Name + "-web",
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
		// only set the key if it doesn't exist so variables set with
		// `flynn env set` are not overwritten
		if _, ok := release.Env[keyVal[0]]; !ok {
			release.Env[keyVal[0]] = keyVal[1]
		}
	}

	if release.Meta == nil {
		release.Meta = make(map[string]string, 1)
	}
	release.Meta["docker-receive"] = "true"

	if err := client.CreateRelease(app.ID, release); err != nil {
		return err
	}
	if err := client.DeployAppRelease(app.ID, release.ID, nil); err != nil {
		return err
	}
	log.Printf("flynn: image deployed, scale it with 'flynn scale app=N'")
	return nil
}

func dockerPush(client controller.Client, repo, tag string) (*ct.Artifact, error) {
	// subscribe to artifact events
	events := make(chan *ct.Event)
	stream, err := client.StreamEvents(ct.StreamEventsOptions{
		ObjectTypes: []ct.EventType{ct.EventTypeArtifact},
	}, events)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	// push the Docker image to docker-receive
	cmd := exec.Command("docker", "push", tag)
	log.Printf("flynn: pushing Docker image with %q", strings.Join(cmd.Args, " "))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	// wait for an artifact to be created
	log.Printf("flynn: image pushed, waiting for artifact creation")
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return nil, fmt.Errorf("event stream closed unexpectedly: %s", stream.Err())
			}
			var artifact ct.Artifact
			if err := json.Unmarshal(event.Data, &artifact); err != nil {
				return nil, err
			}
			if artifact.Meta["docker-receive.repository"] == repo {
				return &artifact, nil
			}
		case <-time.After(30 * time.Second):
			return nil, fmt.Errorf("timed out waiting for artifact creation")
		}
	}

}

func dockerSave(tag string, tw *backup.TarWriter, progress backup.ProgressBar) error {
	tmp, err := ioutil.TempFile("", "flynn-docker-save")
	if err != nil {
		return fmt.Errorf("error creating temp file: %s", err)
	}
	defer tmp.Close()
	defer os.Remove(tmp.Name())

	cmd := exec.Command("docker", "save", tag)
	cmd.Stdout = tmp
	if progress != nil {
		cmd.Stdout = io.MultiWriter(tmp, progress)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	length, err := tmp.Seek(0, os.SEEK_CUR)
	if err != nil {
		return err
	}
	if err := tw.WriteHeader("docker-image.tar", int(length)); err != nil {
		return err
	}
	if _, err := tmp.Seek(0, os.SEEK_SET); err != nil {
		return err
	}
	_, err = io.Copy(tw, tmp)
	return err
}
