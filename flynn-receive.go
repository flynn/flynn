package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"bufio"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/flynn/go-discover/discover"
	"github.com/flynn/lorne/types"
	"github.com/flynn/sampi/client"
	"github.com/flynn/sampi/types"
	"github.com/titanous/go-dockerclient"
)

func main() {
	root := "/var/lib/demo/apps"
	hostname := shell("curl -s icanhazip.com")

	client, err := discover.NewClient()
	if err != nil {
		panic(err)
	}
	set, _ := client.Services("shelf")
	addrs := set.OnlineAddrs()
	if len(addrs) < 1 {
		panic("Shelf is not discoverable")
	}
	shelfHost := addrs[0]

	app := os.Args[2]
	os.MkdirAll(root+"/"+app, 0755)

	fmt.Printf("-----> Building %s on %s ...\n", app, hostname)

	scheduleAndAttach(docker.Config{
		Image:        "flynn/slugbuilder",
		Cmd:          []string{"http://" + shelfHost + "/" + app + ".tgz"},
		Tty:          false,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		StdinOnce:    true,
	})

	/*fmt.Printf("-----> Deploying %s ...\n", app)
		if _, err := os.Stat(root + "/" + app + "/CONTAINER"); err == nil {
	    	oldid := readFile(root + "/" + app + "/CONTAINER")
	    	shell("docker kill " + oldid)
		}

		id := shell("docker run -d -p 5000 -e PORT=5000 -e SLUG_URL=http://"+shelfHost+"/"+app+".tgz flynn/slugrunner start web")
		writeFile(root + "/" + app + "/CONTAINER", id)
		port := shell("docker port "+id+" 5000 | sed 's/0.0.0.0://'")
		writeFile(root + "/" + app + "/PORT", port)
		writeFile(root + "/" + app + "/URL", "http://"+hostname+":"+port)

		fmt.Printf("=====> Application deployed:\n")
		fmt.Printf("       %s\n", readFile(root + "/" + app + "/URL"))*/
	fmt.Println("")

}

func readFile(filename string) string {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func writeFile(filename, data string) {
	err := ioutil.WriteFile(filename, []byte(data), 0644)
	if err != nil {
		panic(err)
	}
}

func shell(cmdline string) string {
	out, err := exec.Command("bash", "-c", cmdline).Output()
	if err != nil {
		panic(err)
	}
	return strings.Trim(string(out), " \n")
}

func attachCmd(cmd *exec.Cmd, stdout, stderr io.Writer, stdin io.Reader) chan error {
	errCh := make(chan error)

	stdinIn, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	stdoutOut, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}
	stderrOut, err := cmd.StderrPipe()
	if err != nil {
		panic(err)
	}

	go func() {
		_, e := io.Copy(stdinIn, stdin)
		errCh <- e
	}()
	go func() {
		_, e := io.Copy(stdout, stdoutOut)
		errCh <- e
	}()
	go func() {
		_, e := io.Copy(stderr, stderrOut)
		errCh <- e
	}()

	return errCh
}

func exitStatusCh(cmd *exec.Cmd) chan uint {
	exitCh := make(chan uint)
	go func() {
		err := cmd.Wait()
		if err != nil {
			if exiterr, ok := err.(*exec.ExitError); ok {
				// There is no plattform independent way to retrieve
				// the exit code, but the following will work on Unix
				if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
					exitCh <- uint(status.ExitStatus())
				}
			} else {
				panic(err)
			}
			return
		}
		exitCh <- uint(0)
	}()
	return exitCh
}

func scheduleAndAttach(config docker.Config) {
	disc, err := discover.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	scheduler, err := client.New()
	if err != nil {
		log.Fatal(err)
	}

	state, err := scheduler.State()
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

	id := randomID()

	services, err := disc.Services("flynn-lorne-attach." + firstHost)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.Dial("tcp", services.OnlineAddrs()[0])
	if err != nil {
		log.Fatal(err)
	}
	err = gob.NewEncoder(conn).Encode(&lorne.AttachReq{
		JobID: id,
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
		HostJobs:    map[string][]*sampi.Job{firstHost: {{ID: id, Config: &config}}},
	}
	if _, err := scheduler.Schedule(schedReq); err != nil {
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
	/*if _, err := io.Copy(os.Stdout, conn); err != nil {
		log.Fatal(err)
	}*/
	conn.Close()
}

func randomID() string {
	b := make([]byte, 16)
	enc := make([]byte, 24)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		panic(err) // This shouldn't ever happen, right?
	}
	base64.URLEncoding.Encode(enc, b)
	return string(bytes.TrimRight(enc, "="))
}
