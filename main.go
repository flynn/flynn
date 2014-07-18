package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"code.google.com/p/go.crypto/ssh"
	"github.com/flynn/flynn-test/cluster"
	"github.com/gorilla/handlers"
	"gopkg.in/check.v1"
)

var listen = flag.Bool("listen", false, "listen for repository events")
var username = flag.String("user", "ubuntu", "user to run QEMU as")
var rootfs = flag.String("rootfs", "rootfs/rootfs.img", "fs image to use with QEMU")
var kernel = flag.String("kernel", "rootfs/vmlinuz", "path to the Linux binary")
var flagCLI = flag.String("cli", "flynn", "path to flynn-cli binary")
var debug = flag.Bool("debug", false, "enable debug output")
var natIface = flag.String("nat", "eth0", "the interface to provide NAT to vms")
var killCluster = flag.Bool("kill", true, "kill the cluster after running the tests")

var sshWrapper = template.Must(template.New("ssh").Parse(`
#!/bin/bash

ssh -o LogLevel=FATAL -o IdentitiesOnly=yes -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i {{.SSHKey}} "$@"
`[1:]))

var gitEnv []string
var dockerfs string
var flynnrc string
var bootConfig cluster.BootConfig
var events chan Event
var githubToken string

var repos = map[string]string{
	"flynn-host":       "master",
	"docker-etcd":      "master",
	"discoverd":        "master",
	"flynn-bootstrap":  "master",
	"flynn-controller": "master",
	"flynn-postgres":   "master",
	"flynn-receive":    "master",
	"shelf":            "master",
	"strowger":         "master",
	"slugbuilder":      "master",
	"slugrunner":       "master",
}

func init() {
	flag.StringVar(&dockerfs, "dockerfs", "", "docker fs")
	flag.StringVar(&flynnrc, "flynnrc", "", "path to flynnrc file")
	flag.Parse()
	log.SetFlags(log.Lshortfile)
}

func main() {
	bootConfig = cluster.BootConfig{
		User:     *username,
		RootFS:   *rootfs,
		Kernel:   *kernel,
		NatIface: *natIface,
	}

	if *listen == true {
		githubToken = os.Getenv("GITHUB_TOKEN")
		if githubToken == "" {
			log.Fatal("GITHUB_TOKEN not set")
		}
		if dockerfs == "" {
			var err error
			if dockerfs, err = cluster.BuildFlynn(bootConfig, "", repos); err != nil {
				log.Fatal("could not build flynn:", err)
			}
		}
		events = make(chan Event, 10)
		go handleEvents(dockerfs)

		http.Handle("/", handlers.CombinedLoggingHandler(os.Stdout, http.HandlerFunc(webhookHandler)))
		fmt.Println("Listening on :80...")
		if err := http.ListenAndServe(":80", nil); err != nil {
			log.Fatal("ListenAndServer: ", err)
		}
	}

	if flynnrc == "" {
		c := cluster.New(bootConfig)
		if dockerfs == "" {
			var err error
			if dockerfs, err = c.BuildFlynn("", repos); err != nil {
				log.Fatal("could not build flynn:", err)
			}
		}
		if err := c.Boot(dockerfs, 1); err != nil {
			log.Fatal("could not boot cluster: ", err)
		}
		if *killCluster {
			defer c.Shutdown()
		}

		if err := createFlynnrc(c); err != nil {
			log.Fatal(err)
		}
		defer os.RemoveAll(flynnrc)
	}

	ssh, err := genSSHKey()
	if err != nil {
		log.Fatal(err)
	}
	defer ssh.Cleanup()
	gitEnv = ssh.Env

	keyAdd := flynn("", "key-add", ssh.Pub)
	if keyAdd.Err != nil {
		log.Fatalf("Error during `%s`:\n%s%s", strings.Join(keyAdd.Cmd, " "), keyAdd.Output, keyAdd.Err)
	}

	res := check.RunAll(&check.RunConf{
		Stream:      true,
		Verbose:     true,
		KeepWorkDir: *debug,
	})
	fmt.Println(res)
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	header, ok := r.Header["X-Github-Event"]
	if !ok {
		log.Println("webhook: request missing X-Github-Event header")
		http.Error(w, "missing X-Github-Event header\n", 400)
		return
	}

	name := strings.Join(header, " ")
	var event Event
	switch name {
	case "push":
		event = &PushEvent{}
	case "pull_request":
		event = &PullRequestEvent{}
	default:
		log.Println("webhook: unknown X-Github-Event:", name)
		http.Error(w, fmt.Sprintf("Unknown X-Github-Event: %s\n", name), 400)
		return
	}

	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&event); err != nil && err != io.EOF {
		log.Println("webhook: error decoding JSON", err)
		http.Error(w, fmt.Sprintf("invalid JSON payload for %s event", name), 400)
		return
	}
	repo := event.Repo()
	if _, ok := repos[repo]; !ok {
		log.Println("webhook: unknown repo", repo)
		http.Error(w, fmt.Sprintf("unknown repo %s", repo), 400)
		return
	}
	logEvent(event)
	events <- event
	io.WriteString(w, "ok\n")
}

func handleEvents(dockerfs string) {
	for event := range events {
		if !needsBuild(event) {
			continue
		}
		updateStatus(event, "pending")
		log.Printf("building %s[%s]\n", event.Repo(), event.Commit())
		repos := map[string]string{event.Repo(): event.Commit()}
		newDockerfs, err := cluster.BuildFlynn(bootConfig, dockerfs, repos)
		if err != nil {
			updateStatus(event, "failure")
			fmt.Printf("could not build flynn: %s\n", err)
			continue
		}

		cmd := exec.Command(
			os.Args[0],
			"--user", *username,
			"--rootfs", *rootfs,
			"--dockerfs", newDockerfs,
			"--kernel", *kernel,
			"--cli", *flagCLI,
			"--nat", *natIface,
			"--kill", "false",
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			updateStatus(event, "failure")
			fmt.Printf("build failed: %s\n", err)
			continue
		}
		updateStatus(event, "success")
		fmt.Printf("build passed!\n")
	}
}

func logEvent(event Event) {
	switch event.(type) {
	case *PushEvent:
		e := event.(*PushEvent)
		log.Printf(
			"received push of %s[%s] by %s: %s => %s\n",
			e.Repo(),
			e.Ref,
			e.Pusher.Name,
			e.Before,
			e.After,
		)
	case *PullRequestEvent:
		e := event.(*PullRequestEvent)
		log.Printf(
			"pull request %s/%d %s by %s\n",
			e.Repo(),
			e.Number,
			e.Action,
			e.Sender.Login,
		)
	}
}

func needsBuild(event Event) bool {
	if e, ok := event.(*PullRequestEvent); ok && e.Action == "closed" {
		return false
	}
	return true
}

type Status struct {
	State       string `json:"state"`
	TargetUrl   string `json:"target_url,omitempty"`
	Description string `json:"description,omitempty"`
	Context     string `json:"context,omitempty"`
}

var descriptions = map[string]string{
	"pending": "The Flynn CI build is in progress",
	"success": "The Flynn CI build passed",
	"failure": "The Flynn CI build failed",
}

func updateStatus(event Event, state string) {
	go func() {
		log.Printf("updateStatus: %s %s[%s]\n", state, event.Repo(), event.Commit())

		url := fmt.Sprintf("https://api.github.com/repos/flynn/%s/statuses/%s", event.Repo(), event.Commit())
		status := Status{State: state, Description: descriptions[state], Context: "flynn"}
		body := bytes.NewBufferString("")
		if err := json.NewEncoder(body).Encode(status); err != nil {
			log.Printf("updateStatus: could not encode status: %+v\n", status)
			return
		}

		req, err := http.NewRequest("POST", url, body)
		if err != nil {
			log.Printf("updateStatus: could not create request: %s\n", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("token %s", githubToken))

		res, err := http.DefaultClient.Do(req)
		defer res.Body.Close()
		if err != nil {
			log.Printf("updateStatus: could not send request: %s\n", err)
			return
		}
		if res.StatusCode != 201 {
			log.Printf("updateStatus: request failed: %d\n", res.StatusCode)
		}
	}()
}

type sshData struct {
	Key     string
	Pub     string
	Env     []string
	Cleanup func()
}

func genSSHKey() (*sshData, error) {
	keyFile, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, err

	}
	defer keyFile.Close()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	pem.Encode(keyFile, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
	})

	pubFile, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, err
	}
	defer pubFile.Close()
	rsaPubKey, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		return nil, err
	}
	if _, err := pubFile.Write(ssh.MarshalAuthorizedKey(rsaPubKey)); err != nil {
		return nil, err
	}

	wrapperFile, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, err
	}
	defer wrapperFile.Close()
	if err := sshWrapper.Execute(wrapperFile, map[string]string{"SSHKey": keyFile.Name()}); err != nil {
		return nil, err
	}
	if err := wrapperFile.Chmod(0700); err != nil {
		return nil, err
	}

	return &sshData{
		Key: keyFile.Name(),
		Pub: pubFile.Name(),
		Env: []string{"GIT_SSH=" + wrapperFile.Name()},
		Cleanup: func() {
			os.RemoveAll(keyFile.Name())
			os.RemoveAll(pubFile.Name())
			os.RemoveAll(wrapperFile.Name())
		},
	}, nil
}

func createFlynnrc(c *cluster.Cluster) error {
	tmpfile, err := ioutil.TempFile("", "flynnrc-")
	if err != nil {
		return err
	}
	flynnrc = tmpfile.Name()

	githost := fmt.Sprintf("%s:2222", c.ControllerDomain)
	url := fmt.Sprintf("https://%s:443", c.ControllerDomain)
	return flynn("", "server-add", "-g", githost, "-p", c.ControllerPin, "default", url, c.ControllerKey).Err
}

type CmdResult struct {
	Cmd    []string
	Output string
	Err    error
}

func flynn(dir string, args ...string) *CmdResult {
	cmd := exec.Command(*flagCLI, args...)
	cmd.Env = append(os.Environ(), "FLYNNRC="+flynnrc)
	cmd.Dir = dir
	return run(cmd)
}

func git(dir string, args ...string) *CmdResult {
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), gitEnv...)
	cmd.Dir = dir
	return run(cmd)
}

func run(cmd *exec.Cmd) *CmdResult {
	var out bytes.Buffer
	if *debug {
		fmt.Println("++", cmd.Path, strings.Join(cmd.Args[1:], " "))
		cmd.Stdout = io.MultiWriter(os.Stdout, &out)
		cmd.Stderr = io.MultiWriter(os.Stderr, &out)
	} else {
		cmd.Stdout = &out
		cmd.Stderr = &out
	}
	err := cmd.Run()
	res := &CmdResult{
		Cmd:    cmd.Args,
		Err:    err,
		Output: out.String(),
	}
	return res
}

var Outputs check.Checker = outputChecker{
	&check.CheckerInfo{
		Name:   "Outputs",
		Params: []string{"result", "output"},
	},
}

type outputChecker struct {
	*check.CheckerInfo
}

func (outputChecker) Check(params []interface{}, names []string) (bool, string) {
	ok, msg, s, res := checkCmdResult(params, names)
	if !ok {
		return ok, msg
	}
	return s == res.Output, ""
}

func checkCmdResult(params []interface{}, names []string) (ok bool, msg, s string, res *CmdResult) {
	res, ok = params[0].(*CmdResult)
	if !ok {
		msg = "result must be a *CmdResult"
		return
	}
	switch v := params[1].(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		msg = "output must be a []byte or string"
		return
	}
	if res.Err != nil {
		return false, "", "", nil
	}
	ok = true
	return
}

var OutputContains check.Checker = outputContainsChecker{
	&check.CheckerInfo{
		Name:   "OutputContains",
		Params: []string{"result", "contains"},
	},
}

type outputContainsChecker struct {
	*check.CheckerInfo
}

func (outputContainsChecker) Check(params []interface{}, names []string) (bool, string) {
	ok, msg, s, res := checkCmdResult(params, names)
	if !ok {
		return ok, msg
	}
	return strings.Contains(res.Output, s), ""
}

var Succeeds check.Checker = succeedsChecker{
	&check.CheckerInfo{
		Name:   "Succeeds",
		Params: []string{"result"},
	},
}

type succeedsChecker struct {
	*check.CheckerInfo
}

func (succeedsChecker) Check(params []interface{}, names []string) (bool, string) {
	res, ok := params[0].(*CmdResult)
	if !ok {
		return false, "result must be a *CmdResult"
	}
	return res.Err == nil, ""
}
