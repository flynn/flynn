package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-dockerclient"
	lc "github.com/flynn/lorne/client"
	"github.com/flynn/lorne/types"
	sc "github.com/flynn/sampi/client"
	"github.com/flynn/sampi/types"
)

// WARNING: assumes one host at the moment

var sd *discoverd.Client
var sched *sc.Client
var host *lc.Client
var hostid string

func init() {
	var err error
	sd, err = discoverd.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	sched, err = sc.New()
	if err != nil {
		log.Fatal(err)
	}

	hostid = findHost()
	host, err = lc.New(hostid)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	root := "/var/lib/demo/apps"

	services, _ := sd.Services("shelf")
	if len(services) < 1 {
		panic("Shelf is not discoverable")
	}
	shelfHost := services[0].Addr

	app := os.Args[2]
	os.MkdirAll(root+"/"+app, 0755)

	fmt.Printf("-----> Building %s...\n", app)

	scheduleAndAttach(sc.RandomJobID(app+"-build."), docker.Config{
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

	jobid := sc.RandomJobID(app + "-web.")

	stopIfExists(jobid)
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

	time.Sleep(1 * time.Second)
	fmt.Printf("=====> Application deployed:\n")
	fmt.Printf("       http://10.0.2.15:%s\n", getPort(jobid))
	fmt.Println("")

}

func shell(cmdline string) string {
	out, err := exec.Command("bash", "-c", cmdline).Output()
	if err != nil {
		panic(err)
	}
	return strings.Trim(string(out), " \n")
}

func stopIfExists(jobid string) {
	_, err := host.GetJob(jobid)
	if err != nil {
		return
	}
	if err := host.StopJob(jobid); err != nil {
		return
	}
}

func scheduleWithTcpPort(jobid string, config docker.Config) {
	schedReq := &sampi.ScheduleReq{
		Incremental: true,
		HostJobs:    map[string][]*sampi.Job{hostid: {{ID: jobid, Config: &config, TCPPorts: 1}}},
	}
	if _, err := sched.Schedule(schedReq); err != nil {
		log.Fatal(err)
	}
}

func getPort(jobid string) string {
	job, err := host.GetJob(jobid)
	if err != nil {
		log.Fatal(err)
	}
	for portspec := range job.Job.Config.ExposedPorts {
		return strings.Split(portspec, "/")[0]
	}
	return ""
}

func findHost() string {
	state, err := sched.State()
	if err != nil {
		log.Fatal(err)
	}

	var firstHost string
	for k := range state {
		firstHost = k
		break
	}
	if firstHost == "" {
		log.Fatal("no hosts")
	}
	return firstHost
}

func scheduleAndAttach(jobid string, config docker.Config) {
	client, err := lc.New(hostid)
	if err != nil {
		log.Fatal(err)
	}
	conn, attachWait, err := client.Attach(&lorne.AttachReq{
		JobID: jobid,
		Flags: lorne.AttachFlagStdout | lorne.AttachFlagStderr | lorne.AttachFlagStdin | lorne.AttachFlagStream,
	}, true)
	if err != nil {
		log.Fatal(err)
	}

	schedReq := &sampi.ScheduleReq{
		Incremental: true,
		HostJobs:    map[string][]*sampi.Job{hostid: {{ID: jobid, Config: &config}}},
	}
	if _, err := sched.Schedule(schedReq); err != nil {
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
		fmt.Fprintln(os.Stdout, scanner.Text()[8:])
	}
	conn.Close()
}
