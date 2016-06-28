package main

import (
	"archive/tar"
	"bytes"
	"errors"
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
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/version"
	"github.com/flynn/go-docopt"
)

func init() {
	log.SetFlags(0)
}

var typesPattern = regexp.MustCompile("types.* -> (.+)\n")

const blobstoreURL = "http://blobstore.discoverd"

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
	if err := run(); err != nil {
		log.Fatalln("ERROR:", err)
	}
}

func run() error {
	client, err := controller.NewClient("", os.Getenv("CONTROLLER_KEY"))
	if err != nil {
		return fmt.Errorf("Unable to connect to controller: %s", err)
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
		return err
	}
	meta, err := parsePairs(args, "--meta")
	if err != nil {
		return err
	}

	slugBuilder, err := client.GetArtifact(os.Getenv("SLUGBUILDER_IMAGE_ID"))
	if err != nil {
		return fmt.Errorf("Error getting slugbuilder image: %s", err)
	}

	slugRunnerID := os.Getenv("SLUGRUNNER_IMAGE_ID")
	if _, err := client.GetArtifact(slugRunnerID); err != nil {
		return fmt.Errorf("Error getting slugrunner image: %s", err)
	}

	app, err := client.GetApp(appName)
	if err == controller.ErrNotFound {
		return fmt.Errorf("Unknown app %q", appName)
	} else if err != nil {
		return fmt.Errorf("Error retrieving app: %s", err)
	}
	prevRelease, err := client.GetAppRelease(app.Name)
	if err == controller.ErrNotFound {
		prevRelease = &ct.Release{}
	} else if err != nil {
		return fmt.Errorf("Error getting current app release: %s", err)
	}

	fmt.Printf("-----> Building %s...\n", app.Name)

	slugImageID := random.UUID()
	jobEnv := map[string]string{
		"BUILD_CACHE_URL": fmt.Sprintf("%s/%s-cache.tgz", blobstoreURL, app.ID),
		"CONTROLLER_KEY":  os.Getenv("CONTROLLER_KEY"),
		"SLUG_IMAGE_ID":   slugImageID,
	}
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

	job := &host.Job{
		Config: host.ContainerConfig{
			Args:       []string{"/builder/build.sh"},
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
		Resources: resource.Defaults(),
	}
	if sb, ok := prevRelease.Processes["slugbuilder"]; ok {
		job.Resources = sb.Resources
	} else if rawLimit := os.Getenv("SLUGBUILDER_DEFAULT_MEMORY_LIMIT"); rawLimit != "" {
		if limit, err := resource.ParseLimit(resource.TypeMemory, rawLimit); err == nil {
			job.Resources[resource.TypeMemory] = resource.Spec{Limit: &limit, Request: &limit}
		}
	}

	cmd := exec.Job(slugBuilder, job)
	cmd.Volumes = []*ct.VolumeReq{{Path: "/tmp", DeleteOnStop: true}}
	var output bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &output)
	cmd.Stderr = os.Stderr

	releaseEnv := make(map[string]string, len(env))
	if prevRelease.Env != nil {
		for k, v := range prevRelease.Env {
			releaseEnv[k] = v
		}
	}
	for k, v := range env {
		releaseEnv[k] = v
	}

	if len(releaseEnv) > 0 {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		go func() {
			if err := appendEnvDir(os.Stdin, stdin, releaseEnv); err != nil {
				log.Fatalln("ERROR:", err)
			}
		}()
	} else {
		cmd.Stdin = os.Stdin
	}

	shutdown.BeforeExit(func() { cmd.Kill() })
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Build failed: %s", err)
	}

	var types []string
	if match := typesPattern.FindSubmatch(output.Bytes()); match != nil {
		types = strings.Split(string(match[1]), ", ")
	}

	fmt.Printf("-----> Creating release...\n")

	release := &ct.Release{
		ArtifactIDs: []string{slugRunnerID, slugImageID},
		Env:         releaseEnv,
		Meta:        prevRelease.Meta,
	}
	if release.Meta == nil {
		release.Meta = make(map[string]string, len(meta))
	}
	for k, v := range meta {
		release.Meta[k] = v
	}
	procs := make(map[string]ct.ProcessType)
	for _, t := range types {
		proc := prevRelease.Processes[t]
		proc.Args = []string{"/runner/init", "start", t}
		if (t == "web" || strings.HasSuffix(t, "-web")) && proc.Service == "" {
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
	if sb, ok := prevRelease.Processes["slugbuilder"]; ok {
		procs["slugbuilder"] = sb
	}
	release.Processes = procs

	if err := client.CreateRelease(app.ID, release); err != nil {
		return fmt.Errorf("Error creating release: %s", err)
	}
	if err := client.DeployAppRelease(app.ID, release.ID, nil); err != nil {
		return fmt.Errorf("Error deploying app release: %s", err)
	}

	// if the app has a web job and has not been scaled before, create a
	// web=1 formation and wait for the "APPNAME-web" service to start
	// (whilst also watching job events so the deploy fails if the job
	// crashes)
	if needsDefaultScale(app.ID, prevRelease.ID, procs, client) {
		fmt.Println("=====> Scaling initial release to web=1")

		formation := &ct.Formation{
			AppID:     app.ID,
			ReleaseID: release.ID,
			Processes: map[string]int{"web": 1},
		}

		jobEvents := make(chan *ct.Job)
		jobStream, err := client.StreamJobEvents(app.ID, jobEvents)
		if err != nil {
			return fmt.Errorf("Error streaming job events: %s", err)
		}
		defer jobStream.Close()

		serviceEvents := make(chan *discoverd.Event)
		serviceStream, err := discoverd.NewService(app.Name + "-web").Watch(serviceEvents)
		if err != nil {
			return fmt.Errorf("Error streaming service events: %s", err)
		}
		defer serviceStream.Close()

		if err := client.PutFormation(formation); err != nil {
			return fmt.Errorf("Error putting formation: %s", err)
		}
		fmt.Println("-----> Waiting for initial web job to start...")

		err = func() error {
			for {
				select {
				case e, ok := <-serviceEvents:
					if !ok {
						return fmt.Errorf("Service stream closed unexpectedly: %s", serviceStream.Err())
					}
					if e.Kind == discoverd.EventKindUp && e.Instance.Meta["FLYNN_RELEASE_ID"] == release.ID {
						fmt.Println("=====> Initial web job started")
						return nil
					}
				case e, ok := <-jobEvents:
					if !ok {
						return fmt.Errorf("Job stream closed unexpectedly: %s", jobStream.Err())
					}
					if e.State == ct.JobStateDown {
						return errors.New("Initial web job failed to start")
					}
				case <-time.After(time.Duration(app.DeployTimeout) * time.Second):
					return errors.New("Timed out waiting for initial web job to start")
				}
			}
		}()
		if err != nil {
			fmt.Println("-----> WARN: scaling initial release down to web=0 due to error")
			formation.Processes["web"] = 0
			if err := client.PutFormation(formation); err != nil {
				// just print this error and return the original error
				fmt.Println("-----> WARN: could not scale the initial release down (it may continue to run):", err)
			}
			return err
		}
	}

	fmt.Println("=====> Application deployed")
	return nil
}

// needsDefaultScale indicates whether a release needs a default scale based on
// whether it has a web process type and either has no previous release or no
// previous scale.
func needsDefaultScale(appID, prevReleaseID string, procs map[string]ct.ProcessType, client controller.Client) bool {
	if _, ok := procs["web"]; !ok {
		return false
	}
	if prevReleaseID == "" {
		return true
	}
	_, err := client.GetFormation(appID, prevReleaseID)
	return err == controller.ErrNotFound
}

func appendEnvDir(stdin io.Reader, pipe io.WriteCloser, env map[string]string) error {
	defer pipe.Close()
	tr := tar.NewReader(stdin)
	tw := tar.NewWriter(pipe)
	defer tw.Close()
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return err
		}
	}
	// append env dir
	for key, value := range env {
		hdr := &tar.Header{
			Name:    path.Join(".ENV_DIR_bdca46b87df0537eaefe79bb632d37709ff1df18", key),
			Mode:    0644,
			ModTime: time.Now(),
			Size:    int64(len(value)),
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write([]byte(value)); err != nil {
			return err
		}
	}
	return nil
}
