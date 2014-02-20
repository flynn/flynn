package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"

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
	services, _ := discoverd.Services("shelf", discoverd.DefaultTimeout)
	if len(services) < 1 {
		panic("Shelf is not discoverable")
	}
	shelfHost := services[0].Addr

	app := os.Args[2]

	fmt.Printf("-----> Building %s...\n", app)

	scheduleAndAttach(cluster.RandomJobID(app+"-build."), docker.Config{
		Image:        "flynn/slugbuilder",
		Cmd:          []string{"http://" + shelfHost + "/" + app + ".tgz"},
		Tty:          false,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		StdinOnce:    true,
	})

	fmt.Printf("-----> Deploying %s ...\n", app)

	jobid := cluster.RandomJobID(app + "-web.")

	scheduleWithTcpPort(jobid, docker.Config{
		Image:        "flynn/slugrunner",
		Cmd:          []string{"start", "web"},
		Tty:          false,
		AttachStdin:  false,
		AttachStdout: false,
		AttachStderr: false,
		OpenStdin:    false,
		StdinOnce:    false,
		Env: []string{
			"SLUG_URL=http://" + shelfHost + "/" + app + ".tgz",
			"SD_NAME=" + app,
		},
	})

	fmt.Println("=====> Application deployed")
}

func scheduleWithTcpPort(jobid string, config docker.Config) (hostid string) {
	hostid = randomHost()
	addReq := &host.AddJobsReq{
		Incremental: true,
		HostJobs:    map[string][]*host.Job{hostid: {{ID: jobid, Config: &config, TCPPorts: 1}}},
	}
	if _, err := clusterc.AddJobs(addReq); err != nil {
		log.Fatal(err)
	}
	return
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
		Incremental: true,
		HostJobs:    map[string][]*host.Job{hostid: {{ID: jobid, Config: &config}}},
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
