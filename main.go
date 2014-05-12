package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"github.com/flynn/go-crypto-ssh"
	"gopkg.in/check.v1"
)

var flagCLI = flag.String("cli", "flynn", "path to flynn-cli binary")
var flagFlynnrc = flag.String("flynnrc", "", "path to flynnrc file")
var debug = flag.Bool("debug", false, "enable debug output")

var sshWrapper = template.Must(template.New("ssh").Parse(`
#!/bin/bash

ssh -o LogLevel=FATAL -o IdentitiesOnly=yes -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i {{.SSHKey}} "$@"
`[1:]))

var gitEnv []string

func init() {
	flag.Parse()
	log.SetFlags(log.Lshortfile)
}

func main() {
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

type CmdResult struct {
	Cmd    []string
	Output string
	Err    error
}

func flynn(dir string, args ...string) *CmdResult {
	cmd := exec.Command(*flagCLI, args...)
	cmd.Env = append(os.Environ(), "FLYNNRC="+*flagFlynnrc)
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

func random() string {
	data := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, data)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(data)
}
