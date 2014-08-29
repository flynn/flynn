package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/code.google.com/p/go.crypto/ssh"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/check.v1"
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

func init() {
	args = arg.Parse()
	log.SetFlags(log.Lshortfile)
}

func main() {
	var err error
	var res *check.Result
	// defer exiting so it runs after all other defers
	defer func() {
		if err != nil || res != nil && !res.Passed() {
			os.Exit(1)
		}
	}()

	flynnrc = args.Flynnrc
	routerIP = args.RouterIP
	if flynnrc == "" {
		var rootFS string
		c := cluster.New(args.BootConfig, os.Stdout)
		rootFS, err = c.BuildFlynn(args.RootFS, "origin/master", false)
		if err != nil {
			c.Shutdown()
			log.Println("could not build flynn: ", err)
			return
		}
		if args.KeepRootFS {
			fmt.Println("Built Flynn in rootfs:", rootFS)
		} else {
			defer os.RemoveAll(rootFS)
		}
		if err = c.Boot(args.Backend, rootFS, 1); err != nil {
			log.Println("could not boot cluster: ", err)
			return
		}
		if args.Kill {
			defer c.Shutdown()
		}

		if err = createFlynnrc(c); err != nil {
			log.Println(err)
			return
		}
		defer os.RemoveAll(flynnrc)

		routerIP = c.RouterIP
	}

	defer func() {
		if args.Debug && (err != nil || res != nil && !res.Passed()) {
			dumpLogs()
		}
	}()

	res = check.RunAll(&check.RunConf{
		Stream:      true,
		Verbose:     true,
		KeepWorkDir: args.Debug,
	})
	fmt.Println(res)
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
	path := tmpfile.Name()

	config, err := c.CLIConfig()
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

func flynn(dir string, cmdArgs ...string) *CmdResult {
	cmd := exec.Command(args.CLI, cmdArgs...)
	cmd.Env = append(os.Environ(), "FLYNNRC="+flynnrc)
	cmd.Dir = dir
	return run(cmd)
}

func run(cmd *exec.Cmd) *CmdResult {
	var out bytes.Buffer
	if args.Debug {
		fmt.Println("++", time.Now().Format("15:04:05.000"), cmd.Path, strings.Join(cmd.Args[1:], " "))
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
	fmt.Println("***** running processes *****")
	run(exec.Command("ps", "faux"))

	fmt.Println("***** flynn-host log *****")
	run(exec.Command("cat", "/tmp/flynn-host.log"))

	whitespace := regexp.MustCompile(`\s+`)
	apps := strings.Split(strings.TrimSpace(flynn("/", "apps").Output), "\n")
	for _, app := range apps[1:] {
		appIdName := whitespace.Split(app, 2)
		ps := strings.Split(strings.TrimSpace(flynn("/", "-a", appIdName[0], "ps").Output), "\n")
		for _, p := range ps[1:] {
			idType := whitespace.Split(p, 2)
			fmt.Println("*****", appIdName[1], idType[1], "log *****")
			flynn("/", "-a", appIdName[0], "log", idType[0])
		}
	}
}
