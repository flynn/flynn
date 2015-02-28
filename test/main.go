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

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/fsouza/go-dockerclient"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/flynn/flynn/pkg/iotool"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/test/arg"
	"github.com/flynn/flynn/test/cluster"
	"github.com/flynn/flynn/test/cluster/client"
)

var sshWrapper = template.Must(template.New("ssh").Parse(`
#!/bin/bash

ssh -o LogLevel=FATAL -o IdentitiesOnly=yes -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i {{.SSHKey}} "$@"
`[1:]))

var args *arg.Args
var flynnrc string
var routerIP string
var testCluster *testcluster.Client

func init() {
	args = arg.Parse()
	if args.Stream {
		args.Debug = true
	}
	log.SetFlags(log.Lshortfile)
}

func main() {
	defer shutdown.Exit()

	var err error
	if err = lookupImageURIs(); err != nil {
		log.Fatalf("could not determine image ID: %s", err)
	}

	var res *check.Result
	// defer exiting here so it runs after all other defers
	defer func() {
		if err != nil || res != nil && !res.Passed() {
			if args.DumpLogs {
				if args.Gist {
					exec.Command("flynn-host", "upload-debug-info").Run()
				} else if testCluster != nil {
					testCluster.DumpLogs(&iotool.SafeWriter{W: os.Stdout})
				}
			}
			os.Exit(1)
		}
	}()

	flynnrc = args.Flynnrc
	routerIP = args.RouterIP
	if flynnrc == "" {
		c := cluster.New(args.BootConfig, os.Stdout)
		var rootFS string
		rootFS, err = c.BuildFlynn(args.RootFS, "origin/master", false, false)
		if err != nil {
			if args.Kill {
				c.Shutdown()
			}
			log.Println("could not build flynn: ", err)
			if rootFS != "" {
				os.RemoveAll(rootFS)
			}
			return
		}
		if args.BuildRootFS {
			c.Shutdown()
			fmt.Println("Built Flynn in rootfs:", rootFS)
			return
		} else {
			defer os.RemoveAll(rootFS)
		}
		if _, err = c.Boot(3, nil, args.Kill); err != nil {
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

	if args.ClusterAPI != "" {
		testCluster, err = testcluster.NewClient(args.ClusterAPI)
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

var imageURIs = map[string]string{
	"test-apps":           "",
	"postgresql":          "",
	"controller-examples": "",
}

func lookupImageURIs() error {
	d, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		return err
	}
	for name := range imageURIs {
		fullName := "flynn/" + name
		image, err := d.InspectImage(fullName)
		if err != nil {
			return err
		}
		imageURIs[name] = fmt.Sprintf("https://example.com?name=%s&id=%s", fullName, image.ID)
	}
	return nil
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
