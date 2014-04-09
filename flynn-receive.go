package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-dockerclient"
	"github.com/flynn/go-flynn/cluster"
)

var clusterc *cluster.Client

func init() {
	var err error
	clusterc, err = cluster.NewClient()
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	var client *controller.Client
	services, _ := discoverd.Services("shelf", discoverd.DefaultTimeout)
	if len(services) < 1 {
		panic("Shelf is not discoverable")
	}
	shelfHost := services[0].Addr

	app := os.Args[1]
	commit := os.Args[2]

	fmt.Printf("-----> Building %s...\n", app)

	types := scheduleAndAttach(cluster.RandomJobID(app+"-build."), docker.Config{
		Image:        "flynn/slugbuilder",
		Cmd:          []string{"http://" + shelfHost + "/" + commit + ".tgz"},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		StdinOnce:    true,
	})

	prevRelease, err := client.GetAppRelease(app)
	if err == controller.ErrNotFound {
		prevRelease = &ct.Release{}
	} else if err != nil {
		log.Fatal(err)
	}
	artifact := &ct.Artifact{URI: "docker://flynn/slugrunner"}
	if err := client.CreateArtifact(artifact); err != nil {
		log.Fatal(err)
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
			proc.Ports.TCP = 1
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
	release.Env["SLUG_URL"] = "http://" + shelfHost + "/" + commit + ".tgz"

	if err := client.CreateRelease(release); err != nil {
		log.Fatal(err)
	}
	if err := client.SetAppRelease(app, release.ID); err != nil {
		log.Fatal(err)
	}

	fmt.Println("=====> Application deployed")
}

func randomHost() (hostid string) {
	hosts, err := clusterc.ListHosts()
	if err != nil {
		log.Fatal(err)
	}

	for hostid = range hosts {
		break
	}
	if hostid == "" {
		log.Fatal("no hosts found")
	}
	return
}

var typesPattern = regexp.MustCompile(`types.* -> (.+)$`)

func scheduleAndAttach(jobid string, config docker.Config) (types []string) {
	hostid := randomHost()

	client, err := clusterc.ConnectHost(hostid)
	if err != nil {
		log.Fatal(err)
	}
	conn, attachWait, err := client.Attach(&host.AttachReq{
		JobID: jobid,
		Flags: host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagStdin | host.AttachFlagStream,
	}, true)
	if err != nil {
		log.Fatal(err)
	}

	addReq := &host.AddJobsReq{
		HostJobs: map[string][]*host.Job{hostid: {{ID: jobid, Config: &config}}},
	}
	if _, err := clusterc.AddJobs(addReq); err != nil {
		log.Fatal(err)
	}

	if err := attachWait(); err != nil {
		log.Fatal(err)
	}

	go func() {
		io.Copy(conn, os.Stdin)
		conn.CloseWrite()
	}()
	scanner := bufio.NewScanner(conn)

	for scanner.Scan() {
		text := scanner.Text()[8:]
		fmt.Fprintln(os.Stdout, text)
		if types == nil {
			if match := typesPattern.FindStringSubmatch(text); match != nil {
				types = strings.Split(match[1], ", ")
			}
		}
	}
	conn.Close()

	return types
}
