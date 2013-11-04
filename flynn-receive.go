package main

import (
	"os/exec"
	"io"
	"os"
	"io/ioutil"
	"strings"
	"syscall"
	"fmt"

	"github.com/flynn/go-discover/discover"	
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
	os.MkdirAll(root + "/" + app, 0755)

	fmt.Printf("-----> Building %s on %s ...\n", app, hostname)

	cmd := exec.Command("docker", "run", "-i", "-a=stdin", "-a=stdout", "flynn/slugbuilder", "http://"+shelfHost+"/"+app+".tgz")
	errCh, startCh := attachCmd(cmd, os.Stdout, os.Stderr, os.Stdin)
	err = cmd.Start()
	if err != nil {
		panic(err)
	}
	close(startCh)
	exitCh := exitStatusCh(cmd)
	if err = <-errCh; err != nil {
		panic(err)
	}
	<-exitCh

	fmt.Printf("-----> Deploying %s ...\n", app)
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
	fmt.Printf("       %s\n", readFile(root + "/" + app + "/URL"))
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


func attachCmd(cmd *exec.Cmd, stdout, stderr io.Writer, stdin io.Reader) (chan error, chan interface{}) {
	errCh := make(chan error)
	startCh := make(chan interface{})

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
		<-startCh
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
	}()

	return errCh, startCh
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