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

	"github.com/flynn/flynn/Godeps/_workspace/src/code.google.com/p/go.crypto/ssh"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/check.v1"
	"github.com/flynn/flynn/test/arg"
	"github.com/flynn/flynn/test/cluster"
)

var sshWrapper = template.Must(template.New("ssh").Parse(`
#!/bin/bash

ssh -o LogLevel=FATAL -o IdentitiesOnly=yes -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i {{.SSHKey}} "$@"
`[1:]))

var gitEnv []string

var args *arg.Args
var flynnrc string
var RouterIP string

func init() {
	args = arg.Parse()
	log.SetFlags(log.Lshortfile)
}

func main() {
	var res *check.Result
	// defer exiting so it runs after all other defers
	defer func() {
		if res != nil && !res.Passed() {
			os.Exit(1)
		}
	}()

	flynnrc = args.Flynnrc
	if flynnrc == "" {
		c := cluster.New(args.BootConfig, os.Stdout)
		rootFS, err := c.BuildFlynn(args.RootFS, "origin/master")
		if err != nil {
			log.Fatal("could not build flynn:", err)
		}
		if args.KeepRootFS {
			fmt.Println("Built Flynn in rootfs:", rootFS)
		} else {
			defer os.RemoveAll(rootFS)
		}
		if err := c.Boot(args.Backend, rootFS, 1); err != nil {
			log.Fatal("could not boot cluster: ", err)
		}
		if args.Kill {
			defer c.Shutdown()
		}

		if err := createFlynnrc(c); err != nil {
			log.Fatal(err)
		}
		defer os.RemoveAll(flynnrc)

		RouterIP = c.RouterIP
	}

	ssh, err := genSSHKey()
	if err != nil {
		log.Fatal(err)
	}
	defer ssh.Cleanup()
	gitEnv = ssh.Env

	keyAdd := flynn("", "key", "add", ssh.Pub)
	if keyAdd.Err != nil {
		log.Fatalf("Error during `%s`:\n%s%s", strings.Join(keyAdd.Cmd, " "), keyAdd.Output, keyAdd.Err)
	}

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

func git(dir string, args ...string) *CmdResult {
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), gitEnv...)
	cmd.Dir = dir
	return run(cmd)
}

func run(cmd *exec.Cmd) *CmdResult {
	var out bytes.Buffer
	if args.Debug {
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
