package main

import (
	"os/exec"
	"strings"
	"encoding/gob"
	"fmt"
	"bufio"
	"io"
	"log"
	"net"
	"time"
	"os"
	
	"github.com/flynn/go-discover/discover"
	"github.com/flynn/lorne/types"
	"github.com/titanous/go-dockerclient"
	"github.com/flynn/sampi/types"
	sc "github.com/flynn/sampi/client"
	lc "github.com/flynn/lorne/client"
)

// WARNING: assumes one host at the moment (firstHost will always be the same)


var sd *discover.Client
var sched *sc.Client
var host *lc.Client
var hostid string

func init() {
	var err error
	sd, err = discover.NewClient()
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
	hostname := shell("curl -s icanhazip.com")

	set, _ := sd.Services("shelf")
	addrs := set.OnlineAddrs()
	if len(addrs) < 1 {
		panic("Shelf is not discoverable")
	}
	shelfHost := addrs[0]

	app := os.Args[2]
	os.MkdirAll(root+"/"+app, 0755)

	fmt.Printf("-----> Building %s on %s ...\n", app, hostname)

	scheduleAndAttach(app + ".build", docker.Config{
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
	
	jobid := app + ".web"

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
		},
	})

	time.Sleep(1 * time.Second)
	fmt.Printf("=====> Application deployed:\n")
	fmt.Printf("       http://%s:%s\n", hostname, getPort(jobid))
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
		return strings.Split(portspec, "/", 1)[0]
	}
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

	services, err := sd.Services("flynn-lorne-attach." + hostid)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.Dial("tcp", services.OnlineAddrs()[0])
	if err != nil {
		log.Fatal(err)
	}
	err = gob.NewEncoder(conn).Encode(&lorne.AttachReq{
		JobID: jobid,
		Flags: lorne.AttachFlagStdout | lorne.AttachFlagStderr | lorne.AttachFlagStdin | lorne.AttachFlagStream,
	})
	if err != nil {
		log.Fatal(err)
	}
	attachState := make([]byte, 1)
	if _, err := conn.Read(attachState); err != nil {
		log.Fatal(err)
	}
	switch attachState[0] {
	case lorne.AttachError:
		log.Fatal("attach error")
	}

	schedReq := &sampi.ScheduleReq{
		Incremental: true,
		HostJobs:    map[string][]*sampi.Job{hostid: {{ID: jobid, Config: &config}}},
	}
	if _, err := sched.Schedule(schedReq); err != nil {
		log.Fatal(err)
	}

	if _, err := conn.Read(attachState); err != nil {
		log.Fatal(err)
	}

	go func() {
		io.Copy(conn, os.Stdin)
		conn.(*net.TCPConn).CloseWrite()
	}()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		fmt.Fprintln(os.Stdout, scanner.Text()[8:])
	}
	conn.Close()
}
