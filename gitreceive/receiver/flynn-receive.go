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

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/version"
)

var clusterc = cluster.NewClient()

func init() {
	log.SetFlags(0)
}

var typesPattern = regexp.MustCompile("types.* -> (.+)\n")

const blobstoreURL = "http://blobstore.discoverd"
const scaleTimeout = 20 * time.Second

func parsePairs(args *docopt.Args, str string) (map[string]string, error) {
	pairs := args.All[str].([]string)
	item := make(map[string]string, len(pairs))
	for _, s := range pairs {
		v := strings.SplitN(s, "=", 2)
		if len(v) != 2 {
			return nil, fmt.Errorf("invalid var format: %q", s)
		}
		item[v[0]] = v[1]
	}
	return item, nil
}

func main() {
	client, err := controller.NewClient("", os.Getenv("CONTROLLER_KEY"))
	if err != nil {
		log.Fatalln("Unable to connect to controller:", err)
	}

	usage := `
Usage: flynn-receiver <app> <rev> [-e <var>=<val>]... [-m <key>=<val>]...

Options:
	-e,--env <var>=<val>
	-m,--meta <key>=<val>
`[1:]
	args, _ := docopt.Parse(usage, nil, true, version.String(), false)

	appName := args.String["<app>"]
	env, err := parsePairs(args, "--env")
	if err != nil {
		log.Fatal(err)
	}
	meta, err := parsePairs(args, "--meta")
	if err != nil {
		log.Fatal(err)
	}

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

	jobEnv := make(map[string]string)
	jobEnv["BUILD_CACHE_URL"] = fmt.Sprintf("%s/%s-cache.tgz", blobstoreURL, app.ID)
	if buildpackURL, ok := env["BUILDPACK_URL"]; ok {
		jobEnv["BUILDPACK_URL"] = buildpackURL
	} else if buildpackURL, ok := prevRelease.Env["BUILDPACK_URL"]; ok {
		jobEnv["BUILDPACK_URL"] = buildpackURL
	}
	for _, k := range []string{"SSH_CLIENT_KEY", "SSH_CLIENT_HOSTS"} {
		if v := os.Getenv(k); v != "" {
			jobEnv[k] = v
		}
	}
	slugURL := fmt.Sprintf("%s/%s/slug.tgz", blobstoreURL, random.UUID())

	cmd := exec.Job(exec.DockerImage(os.Getenv("SLUGBUILDER_IMAGE_URI")), &host.Job{
		Config: host.ContainerConfig{
			Cmd:        []string{slugURL},
			Env:        jobEnv,
			Stdin:      true,
			DisableLog: true,
		},
		Partition: "background",
		Metadata: map[string]string{
			"flynn-controller.app":      app.ID,
			"flynn-controller.app_name": app.Name,
			"flynn-controller.release":  prevRelease.ID,
			"flynn-controller.type":     "slugbuilder",
		},
	})
	var output bytes.Buffer
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

	if err := cmd.Run(); err != nil {
		log.Fatalln("Build failed:", err)
	}

	var types []string
	if match := typesPattern.FindSubmatch(output.Bytes()); match != nil {
		types = strings.Split(string(match[1]), ", ")
	}

	fmt.Printf("-----> Creating release...\n")

	artifact := &ct.Artifact{Type: host.ArtifactTypeDocker, URI: os.Getenv("SLUGRUNNER_IMAGE_URI")}
	if err := client.CreateArtifact(artifact); err != nil {
		log.Fatalln("Error creating image artifact:", err)
	}

	slugArtifact := &ct.Artifact{
		Type: host.ArtifactTypeFile,
		URI:  slugURL,
		Meta: map[string]string{"blobstore": "true"},
	}
	if err := client.CreateArtifact(slugArtifact); err != nil {
		log.Fatalln("Error creating slug artifact:", err)
	}

	release := &ct.Release{
		ArtifactIDs: []string{artifact.ID, slugArtifact.ID},
		Env:         prevRelease.Env,
		Meta:        prevRelease.Meta,
	}
	if release.Meta == nil {
		release.Meta = make(map[string]string, len(meta))
	}
	if release.Env == nil {
		release.Env = make(map[string]string, len(env))
	}
	for k, v := range env {
		release.Env[k] = v
	}
	for k, v := range meta {
		release.Meta[k] = v
	}
	procs := make(map[string]ct.ProcessType)
	for _, t := range types {
		proc := prevRelease.Processes[t]
		proc.Cmd = []string{"start", t}
		if t == "web" || strings.HasSuffix(t, "-web") {
			proc.Service = app.Name + "-" + t
			proc.Ports = []ct.Port{{
				Port:  8080,
				Proto: "tcp",
				Service: &host.Service{
					Name:   proc.Service,
					Create: true,
					Check:  &host.HealthCheck{Type: "tcp"},
				},
			}}
		}
		procs[t] = proc
	}
	release.Processes = procs

	if err := client.CreateRelease(release); err != nil {
		log.Fatalln("Error creating release:", err)
	}
	if err := client.DeployAppRelease(app.Name, release.ID); err != nil {
		log.Fatalln("Error deploying app release:", err)
	}

	fmt.Println("=====> Application deployed")

	if needsDefaultScale(app.ID, prevRelease.ID, procs, client) {
		formation := &ct.Formation{
			AppID:     app.ID,
			ReleaseID: release.ID,
			Processes: map[string]int{"web": 1},
		}

		watcher, err := client.WatchJobEvents(app.ID, release.ID)
		if err != nil {
			log.Fatalln("Error streaming job events", err)
			return
		}
		defer watcher.Close()

		if err := client.PutFormation(formation); err != nil {
			log.Fatalln("Error putting formation:", err)
		}
		fmt.Println("=====> Waiting for web job to start...")

		err = watcher.WaitFor(ct.JobEvents{"web": ct.JobUpEvents(1)}, scaleTimeout, func(e *ct.Job) error {
			switch e.State {
			case ct.JobStateUp:
				fmt.Println("=====> Default web formation scaled to 1")
			case ct.JobStateDown:
				return fmt.Errorf("Failed to scale web process type")
			}
			return nil
		})
		if err != nil {
			log.Fatalln(err.Error())
		}
	}
}

// needsDefaultScale indicates whether a release needs a default scale based on
// whether it has a web process type and either has no previous release or no
// previous scale.
func needsDefaultScale(appID, prevReleaseID string, procs map[string]ct.ProcessType, client *controller.Client) bool {
	if _, ok := procs["web"]; !ok {
		return false
	}
	if prevReleaseID == "" {
		return true
	}
	_, err := client.GetFormation(appID, prevReleaseID)
	return err == controller.ErrNotFound
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
			Mode:    0644,
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
