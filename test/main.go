package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/fsouza/go-dockerclient"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/flynn/flynn/test/arg"
	"github.com/flynn/flynn/test/cluster"
)

var sshWrapper = template.Must(template.New("ssh").Parse(`
#!/bin/bash

ssh -o LogLevel=FATAL -o IdentitiesOnly=yes -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i {{.SSHKey}} "$@"
`[1:]))

var args *arg.Args
var flynnrc string
var routerIP string
var testCluster *cluster.Cluster
var httpClient *http.Client
var testImageURIs map[string]string
var testImageNames = []string{"test-apps", "controller-examples"}

func init() {
	args = arg.Parse()
	if args.Stream {
		args.Debug = true
	}
	log.SetFlags(log.Lshortfile)
}

func main() {
	if err := lookupImages(testImageNames); err != nil {
		log.Fatal(err)
	}

	var err error
	var res *check.Result
	// defer exiting here so it runs after all other defers
	defer func() {
		if err != nil || res != nil && !res.Passed() {
			if args.Debug {
				if args.Gist {
					exec.Command("flynn-host", "upload-debug-info").Run()
				} else {
					dumpLogs()
				}
			}
			os.Exit(1)
		}
	}()

	flynnrc = args.Flynnrc
	routerIP = args.RouterIP
	if flynnrc == "" {
		var rootFS string
		testCluster = cluster.New(args.BootConfig, os.Stdout)
		rootFS, err = testCluster.BuildFlynn(args.RootFS, "origin/master", false)
		if err != nil {
			testCluster.Shutdown()
			log.Println("could not build flynn: ", err)
			return
		}
		if args.KeepRootFS {
			fmt.Println("Built Flynn in rootfs:", rootFS)
		} else {
			defer os.RemoveAll(rootFS)
		}
		if err = testCluster.Boot(rootFS, 3); err != nil {
			log.Println("could not boot cluster: ", err)
			return
		}
		if args.Kill {
			defer testCluster.Shutdown()
		}

		if err = createFlynnrc(); err != nil {
			log.Println(err)
			return
		}
		defer os.RemoveAll(flynnrc)

		routerIP = testCluster.RouterIP
	}

	if args.ClusterAPI != "" {
		httpClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{ServerName: "ci.flynn.io"}}}

		res, err := httpClient.Get(args.ClusterAPI)
		if err != nil {
			log.Println(err)
			return
		}
		testCluster = &cluster.Cluster{}
		err = json.NewDecoder(res.Body).Decode(testCluster)
		res.Body.Close()
		if err != nil {
			log.Println(err)
			return
		}
	}

	res = check.RunAll(&check.RunConf{
		Filter:           args.Run,
		Stream:           args.Stream,
		Verbose:          args.Debug,
		KeepWorkDir:      args.Debug,
		ConcurrencyLevel: 5,
	})
	fmt.Println(res)
}

func lookupImages(names []string) error {
	testImageURIs = make(map[string]string, len(names))
	for _, name := range names {
		id, err := lookupImageID("flynn/" + name)
		if err != nil {
			return fmt.Errorf("could not determine %s image ID: %s", name, err)
		}
		testImageURIs[name] = fmt.Sprintf("https://example.com/%s?id=%s", name, id)
	}
	return nil
}

func lookupImageID(name string) (string, error) {
	d, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		return "", err
	}
	image, err := d.InspectImage(name)
	if err != nil {
		return "", err
	}
	return image.ID, nil
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

func createFlynnrc() error {
	tmpfile, err := ioutil.TempFile("", "flynnrc-")
	if err != nil {
		return err
	}
	path := tmpfile.Name()

	config, err := testCluster.CLIConfig()
	if err != nil {
		os.RemoveAll(path)
		return err
	}

	if err := config.SaveTo(path); err != nil {
		os.RemoveAll(path)
		return err
	}

	flynnrc = path
	return nil
}

type CmdResult struct {
	Cmd    []string
	Output string
	Err    error
}

func flynnEnv(path string) []string {
	env := os.Environ()
	res := make([]string, 0, len(env)+1)
	for _, v := range env {
		if !strings.HasPrefix(v, "FLYNNRC=") {
			res = append(res, v)
		}
	}
	res = append(res, "FLYNNRC="+path)
	return res
}

func flynnCmd(dir string, cmdArgs ...string) *exec.Cmd {
	cmd := exec.Command(args.CLI, cmdArgs...)
	cmd.Env = flynnEnv(flynnrc)
	cmd.Dir = dir
	return cmd
}

func flynn(t *check.C, dir string, args ...string) *CmdResult {
	return run(t, flynnCmd(dir, args...))
}

func debug(t *check.C, v ...interface{}) {
	t.Log(append([]interface{}{"++ ", time.Now().Format("15:04:05.000"), " "}, v...)...)
}

func debugf(t *check.C, format string, v ...interface{}) {
	t.Logf(strings.Join([]string{"++", time.Now().Format("15:04:05.000"), format}, " "), v...)
}

func run(t *check.C, cmd *exec.Cmd) *CmdResult {
	var out bytes.Buffer
	debug(t, strings.Join(append([]string{cmd.Path}, cmd.Args[1:]...), " "))
	if args.Stream {
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
	if !args.Stream {
		t.Log(res.Output)
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

type matchesChecker struct {
	*check.CheckerInfo
}

var Matches check.Checker = &matchesChecker{
	&check.CheckerInfo{Name: "Matches", Params: []string{"value", "regex"}},
}

func (checker *matchesChecker) Check(params []interface{}, names []string) (result bool, error string) {
	return matches(params[0], params[1])
}

func matches(value, regex interface{}) (result bool, error string) {
	reStr, ok := regex.(string)
	if !ok {
		return false, "Regex must be a string"
	}
	valueStr, valueIsStr := value.(string)
	if !valueIsStr {
		if valueWithStr, valueHasStr := value.(fmt.Stringer); valueHasStr {
			valueStr, valueIsStr = valueWithStr.String(), true
		}
	}
	if valueIsStr {
		matches, err := regexp.MatchString(reStr, valueStr)
		if err != nil {
			return false, "Can't compile regex: " + err.Error()
		}
		return matches, ""
	}
	return false, "Obtained value is not a string and has no .String()"
}

func dumpLogs() {
	run := func(cmd *exec.Cmd) string {
		fmt.Println(cmd.Path, strings.Join(cmd.Args[1:], " "))
		var out bytes.Buffer
		cmd.Stdout = io.MultiWriter(os.Stdout, &out)
		cmd.Stderr = io.MultiWriter(os.Stderr, &out)
		cmd.Run()
		return out.String()
	}

	fmt.Println("***** running processes *****")
	run(exec.Command("ps", "faux"))

	fmt.Println("***** flynn-host log *****")
	run(exec.Command("cat", "/tmp/flynn-host.log"))

	ids := strings.Split(strings.TrimSpace(run(exec.Command("flynn-host", "ps", "-a", "-q"))), "\n")
	for _, id := range ids {
		fmt.Print("\n\n***** ***** ***** ***** ***** ***** ***** ***** ***** *****\n\n")
		run(exec.Command("flynn-host", "inspect", id))
		fmt.Println()
		run(exec.Command("flynn-host", "log", id))
	}
}
