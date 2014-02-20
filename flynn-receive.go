package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

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
	root := "/var/lib/demo/apps"

	services, _ := discoverd.Services("shelf", discoverd.DefaultTimeout)
	if len(services) < 1 {
		panic("Shelf is not discoverable")
	}
	shelfHost := services[0].Addr

	app := os.Args[2]
	os.MkdirAll(root+"/"+app, 0755)

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

	hostid := scheduleWithTcpPort(jobid, docker.Config{
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

	time.Sleep(1 * time.Second)
	fmt.Printf("=====> Application deployed:\n")
	fmt.Printf("       http://10.0.2.15:%s\n", getPort(hostid, jobid))
	fmt.Println("")

}

func shell(cmdline string) string {
	out, err := exec.Command("bash", "-c", cmdline).Output()
	if err != nil {
		panic(err)
	}
	return strings.Trim(string(out), " \n")
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

func getPort(hostid string, jobid string) string {
	client, err := clusterc.ConnectHost(hostid)
	if err != nil {
		log.Fatal(err)
	}
	job, err := client.GetJob(jobid)
	if err != nil {
		log.Fatal(err)
	}
	for portspec := range job.Job.Config.ExposedPorts {
		return strings.Split(portspec, "/")[0]
	}
	return ""
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
