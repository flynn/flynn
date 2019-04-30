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
	"path/filepath"
	"strings"
	"time"

	"github.com/cheggaaa/pb"
	cfg "github.com/flynn/flynn/cli/config"
	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	host "github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/archive"
	"github.com/flynn/flynn/pkg/backup"
	"github.com/flynn/flynn/pkg/term"
	"github.com/flynn/flynn/pkg/version"
	tarclient "github.com/flynn/flynn/tarreceive/client"
	"github.com/flynn/go-docopt"
)

func init() {
	register("docker", runDocker, `
usage: flynn docker push <image>
       flynn docker set-push-url [<url>]
       flynn docker login
       flynn docker logout


Deploy Docker images to a Flynn cluster.

Commands:
	push          push and release a Docker image to the cluster

	set-push-url  [DEPRECATED] set the Docker push URL (defaults to https://docker.$CLUSTER_DOMAIN)

	login         [DEPRECATED] run "docker login" against the cluster's docker-receive app

	logout        [DEPRECATED] run "docker logout" against the cluster's docker-receive app

Example:

	Assuming you have a Docker image tagged "my-custom-image:v2":

	$ flynn docker push my-custom-image:v2
	deploying Docker image: my-custom-image:v2
	exporting image with 'docker save my-custom-image:v2'
	111.58 MB 109.70 MB/s 1s
	uploading layer fccbfa2912f0cd6b9d13f91f288f112a2b825f3f758a4443aacb45bfc108cc74
	111.52 MB 25.56 MB/s 4s
	uploading layer e1a9a6284d0d24d8194ac84b372619e75cd35a46866b74925b7274c7056561e4
	15.50 KB 620.05 KB/s 0s
	uploading layer ac7299292f8b2f710d3b911c6a4e02ae8f06792e39822e097f9c4e9c2672b32d
	14.50 KB 601.45 KB/s 0s
	uploading layer a5e66470b2812e91798db36eb103c1f1e135bbe167e4b2ad5ba425b8db98ee8d
	5.50 KB 279.83 KB/s 0s
	uploading layer a8de0e025d94b33db3542e1e8ce58829144b30c6cd1fff057eec55b1491933c3
	3.00 KB 153.83 KB/s 0s
	Docker image deployed, scale it with 'flynn scale app=N'
`)
}

// minDockerPushTarVersion is the minimum API version which supports pushing
// Docker images as tar layers.
const minDockerPushTarVersion = "v20190425.0"

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
	fmt.Fprintf(os.Stderr, "DEPRECATED: Pushing via a Docker registry has been deprecated in favour of pushing via the Flynn image service.\nIf the cluster is newer than %s then just run 'flynn docker push' directly.\n", minDockerPushTarVersion)
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
	fmt.Fprintf(os.Stderr, "DEPRECATED: Pushing via a Docker registry has been deprecated in favour of pushing via the Flynn image service.\nIf the cluster is newer than %s then just run 'flynn docker push' directly.\n", minDockerPushTarVersion)
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
	fmt.Fprintf(os.Stderr, "DEPRECATED: Pushing via a Docker registry has been deprecated in favour of pushing via the Flynn image service.\nIf the cluster is newer than %s then just run 'flynn docker push' directly.\n", minDockerPushTarVersion)
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
	return dockerLoginWithEmail(host, key, false)
}

func dockerLoginWithEmail(host, key string, useEmail bool) error {
	flags := []string{"--username=user", "--password=" + key}
	if useEmail {
		flags = append(flags, "--email=user@"+host)
	}
	cmd := exec.Command("docker", append([]string{"login"}, append(flags, host)...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	switch {
	case !useEmail && strings.Contains(out.String(), "Email: EOF"):
		return dockerLoginWithEmail(host, key, true)
	case strings.Contains(out.String(), "certificate signed by unknown authority"):
		return ErrDockerTLSError
	case err != nil:
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
	status, err := client.Status()
	if err != nil {
		return err
	}
	v := version.Parse(status.Version)
	if !v.Dev && v.Before(version.Parse(minDockerPushTarVersion)) {
		fmt.Fprintf(os.Stderr, "DEPRECATED: Pushing via a Docker registry has been deprecated in favour of pushing via the Flynn image service.\nConsider upgrading your cluster to a version newer than %s.\n", minDockerPushTarVersion)
		return runDockerPushLegacy(args, client)
	}
	return runDockerPushTar(args, client)
}

func runDockerPushLegacy(args *docopt.Args, client controller.Client) error {
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
		proc.Service = app.Name + "-web"
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

	length, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if err := tw.WriteHeader("docker-image.tar", int(length)); err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = io.Copy(tw, tmp)
	return err
}

// DockerManifest is used to read manifest.json from the output of
// 'docker save'.
type DockerManifest struct {
	Config string   `json:"Config"`
	Layers []string `json:"Layers"`
}

// DockerConfig is used to read image config from the output of 'docker save'.
type DockerConfig struct {
	Config struct {
		Env        []string
		Cmd        []string
		WorkingDir string
		Entrypoint []string
	} `json:"config"`
	Rootfs struct {
		Diffs []string `json:"diff_ids"`
	} `json:"rootfs"`
}

func runDockerPushTar(args *docopt.Args, client controller.Client) error {
	tag := args.String["<image>"]
	log.Printf("deploying Docker image: %s", tag)

	tarClient, err := clusterConf.TarClient()
	if err != nil {
		return err
	}

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

	log.Printf("exporting image with 'docker save %s'", tag)
	cmd := exec.Command("docker", "save", tag)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	tmpDir, err := ioutil.TempDir("", "flynn-docker-push")
	if err != nil {
		return fmt.Errorf("error creating temporary directory: %s", err)
	}
	defer os.RemoveAll(tmpDir)
	if err := func() error {
		var src io.Reader = stdout
		if term.IsTerminal(os.Stderr.Fd()) {
			bar := pb.New(0)
			bar.SetUnits(pb.U_BYTES)
			bar.ShowBar = true
			bar.ShowSpeed = true
			bar.Output = os.Stderr
			bar.Start()
			defer bar.Finish()
			src = io.TeeReader(src, bar)
		}
		return archive.Unpack(src, tmpDir, false)
	}(); err != nil {
		return fmt.Errorf("error extracting docker save output: %s", err)
	}

	// read the manifest
	manifest, err := func() (*DockerManifest, error) {
		f, err := os.Open(filepath.Join(tmpDir, "manifest.json"))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		var manifests []*DockerManifest
		if err := json.NewDecoder(f).Decode(&manifests); err != nil {
			return nil, err
		}
		if len(manifests) != 1 {
			return nil, fmt.Errorf("expected 1 docker manifest, got %d", len(manifests))
		}
		return manifests[0], nil
	}()
	if err != nil {
		return fmt.Errorf("error loading docker manifest: %s", err)
	}

	// read the config
	config, err := func() (*DockerConfig, error) {
		f, err := os.Open(filepath.Join(tmpDir, manifest.Config))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		var config DockerConfig
		return &config, json.NewDecoder(f).Decode(&config)
	}()
	if err != nil {
		return fmt.Errorf("error loading docker image config: %s", err)
	}

	// upload each layer
	layers := make([]*ct.ImageLayer, len(manifest.Layers))
	for i, path := range manifest.Layers {
		diffID := config.Rootfs.Diffs[i]
		p := strings.SplitN(diffID, ":", 2)
		if len(p) != 2 {
			return fmt.Errorf("invalid diff ID: %s", diffID)
		}
		id := p[1]
		log.Printf("uploading layer %s", id)
		layer, err := tarClient.GetLayer(id)
		if err == tarclient.ErrNotFound {
			if err := func() (err error) {
				f, err := os.Open(filepath.Join(tmpDir, path))
				if err != nil {
					return err
				}
				defer f.Close()
				var src io.Reader = f
				if term.IsTerminal(os.Stderr.Fd()) {
					bar := pb.New(0)
					bar.SetUnits(pb.U_BYTES)
					bar.ShowBar = true
					bar.ShowSpeed = true
					bar.Output = os.Stderr
					bar.Start()
					defer bar.Finish()
					src = io.TeeReader(src, bar)
				}
				layer, err = tarClient.CreateLayer(id, src)
				return
			}(); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		layers[i] = layer
	}

	// generate the image manifest
	entrypoint := &ct.ImageEntrypoint{
		WorkingDir: config.Config.WorkingDir,
		Env:        make(map[string]string, len(config.Config.Env)),
		Args:       append(config.Config.Entrypoint, config.Config.Cmd...),
	}
	for _, env := range config.Config.Env {
		keyVal := strings.SplitN(env, "=", 2)
		if len(keyVal) != 2 {
			continue
		}
		val := strings.Replace(keyVal[1], "\t", "\\t", -1)
		entrypoint.Env[keyVal[0]] = val
	}
	image := &ct.ImageManifest{
		Type:        ct.ImageManifestTypeV1,
		Entrypoints: map[string]*ct.ImageEntrypoint{"_default": entrypoint},
		Rootfs: []*ct.ImageRootfs{{
			Platform: ct.DefaultImagePlatform,
			Layers:   layers,
		}},
	}

	// create the artifact
	artifact, err := tarClient.CreateArtifact(image)
	if err != nil {
		return err
	}

	// create and deploy a release with the image config and created artifact
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
	proc.Args = entrypoint.Args
	if len(proc.Ports) == 0 {
		proc.Service = app.Name + "-web"
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

	if len(config.Config.Env) > 0 && release.Env == nil {
		release.Env = make(map[string]string, len(config.Config.Env))
	}
	for _, v := range config.Config.Env {
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

	if err := client.CreateRelease(app.ID, release); err != nil {
		return err
	}
	if err := client.DeployAppRelease(app.ID, release.ID, nil); err != nil {
		return err
	}
	log.Printf("Docker image deployed, scale it with 'flynn scale app=N'")
	return nil
}
