package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/flynn/flynn/cli/config"
	cc "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/random"
)

type CLISuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&CLISuite{})

func (s *CLISuite) flynn(t *c.C, args ...string) *CmdResult {
	return flynn(t, "/", args...)
}

func (s *CLISuite) newCliTestApp(t *c.C) *cliTestApp {
	app, release := s.createApp(t)
	watcher, err := s.controllerClient(t).WatchJobEvents(app.Name, release.ID)
	t.Assert(err, c.IsNil)
	return &cliTestApp{
		name:    app.Name,
		disc:    s.discoverdClient(t),
		t:       t,
		watcher: watcher,
	}
}

type cliTestApp struct {
	name    string
	watcher *cc.JobWatcher
	disc    *discoverd.Client
	t       *c.C
}

func (a *cliTestApp) flynn(args ...string) *CmdResult {
	return flynn(a.t, "/", append([]string{"-a", a.name}, args...)...)
}

func (a *cliTestApp) flynnCmd(args ...string) *exec.Cmd {
	return flynnCmd("/", append([]string{"-a", a.name}, args...)...)
}

func (a *cliTestApp) waitFor(events ct.JobEvents) string {
	var id string
	idSetter := func(e *ct.Job) error {
		id = e.ID
		return nil
	}

	a.t.Assert(a.watcher.WaitFor(events, scaleTimeout, idSetter), c.IsNil)
	return id
}

func (a *cliTestApp) waitForService(name string) {
	_, err := a.disc.Instances(name, 30*time.Second)
	a.t.Assert(err, c.IsNil)
}

func (a *cliTestApp) sh(cmd string) *CmdResult {
	return a.flynn("run", "sh", "-c", cmd)
}

func (a *cliTestApp) cleanup() {
	a.watcher.Close()
}

func (s *CLISuite) TestCreateAppNoGit(t *c.C) {
	dir := t.MkDir()
	name := random.String(30)
	t.Assert(flynn(t, dir, "create", name), Outputs, fmt.Sprintf("Created %s\n", name))
}

func testApp(s *CLISuite, t *c.C, remote string) {
	app := s.newGitRepo(t, "")
	name := random.String(30)
	flynnRemote := fmt.Sprintf("%s\tssh://git@%s/%s.git (push)", remote, s.clusterConf(t).GitHost, name)

	if remote == "flynn" {
		t.Assert(app.flynn("create", "-y", name), Outputs, fmt.Sprintf("Created %s\n", name))
	} else {
		t.Assert(app.flynn("create", "-r", remote, "-y", name), Outputs, fmt.Sprintf("Created %s\n", name))
	}
	t.Assert(app.flynn("apps"), SuccessfulOutputContains, name)
	t.Assert(app.flynn("-c", "default", "apps"), SuccessfulOutputContains, name)
	if remote == "" {
		t.Assert(app.git("remote", "-v"), c.Not(SuccessfulOutputContains), flynnRemote)
	} else {
		t.Assert(app.git("remote", "-v"), SuccessfulOutputContains, flynnRemote)
	}

	// make sure flynn components are listed
	t.Assert(app.flynn("apps"), SuccessfulOutputContains, "router")
	t.Assert(app.flynn("-c", "default", "apps"), SuccessfulOutputContains, "router")

	// flynn delete
	if remote == "flynn" {
		t.Assert(app.flynn("delete", "--yes"), Succeeds)
	} else {
		if remote == "" {
			t.Assert(app.flynn("-a", name, "delete", "--yes", "-r", remote), Succeeds)
		} else {
			t.Assert(app.flynn("delete", "--yes", "-r", remote), Succeeds)
		}
	}
	t.Assert(app.git("remote", "-v"), c.Not(SuccessfulOutputContains), flynnRemote)
}

func (s *CLISuite) TestApp(t *c.C) {
	testApp(s, t, "flynn")
}

func (s *CLISuite) TestAppWithCustomRemote(t *c.C) {
	testApp(s, t, random.String(8))
}

func (s *CLISuite) TestAppWithNoRemote(t *c.C) {
	testApp(s, t, "")
}

// TODO: share with cli/key.go
func formatKeyID(s string) string {
	buf := make([]byte, 0, len(s)+((len(s)-2)/2))
	for i := range s {
		buf = append(buf, s[i])
		if (i+1)%2 == 0 && i != len(s)-1 {
			buf = append(buf, ':')
		}
	}
	return string(buf)
}

func (s *CLISuite) TestKey(t *c.C) {
	app := s.newGitRepo(t, "empty")
	t.Assert(app.flynn("create"), Succeeds)

	t.Assert(app.flynn("key", "add", s.sshKeys(t).Pub), Succeeds)

	// calculate fingerprint
	data, err := ioutil.ReadFile(s.sshKeys(t).Pub)
	t.Assert(err, c.IsNil)
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(data)
	t.Assert(err, c.IsNil)
	digest := md5.Sum(pubKey.Marshal())
	fingerprint := formatKeyID(hex.EncodeToString(digest[:]))

	t.Assert(app.flynn("key"), SuccessfulOutputContains, fingerprint)

	t.Assert(app.git("commit", "--allow-empty", "-m", "should succeed"), Succeeds)
	t.Assert(app.git("push", "flynn", "master"), Succeeds)

	t.Assert(app.flynn("key", "remove", fingerprint), Succeeds)
	t.Assert(app.flynn("key"), c.Not(SuccessfulOutputContains), fingerprint)

	t.Assert(app.git("commit", "--allow-empty", "-m", "should fail"), Succeeds)
	t.Assert(app.git("push", "flynn", "master"), c.Not(Succeeds))

	t.Assert(app.flynn("delete", "--yes"), Succeeds)
}

func (s *CLISuite) TestPs(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	ps := func() []string {
		out := app.flynn("ps")
		t.Assert(out, Succeeds)
		lines := strings.Split(out.Output, "\n")
		return lines[1 : len(lines)-1]
	}
	// empty formation == empty ps
	t.Assert(ps(), c.HasLen, 0)
	t.Assert(app.flynn("scale", "echoer=3"), Succeeds)
	jobs := ps()
	// should return 3 jobs
	t.Assert(jobs, c.HasLen, 3)
	// check job types
	for _, j := range jobs {
		t.Assert(j, Matches, "echoer")
	}
	t.Assert(app.flynn("scale", "echoer=0"), Succeeds)
	t.Assert(ps(), c.HasLen, 0)
}

func (s *CLISuite) TestScale(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()

	assertEventOutput := func(scale *CmdResult, events ct.JobEvents) {
		var actual []*ct.Job
		f := func(e *ct.Job) error {
			actual = append(actual, e)
			return nil
		}
		t.Assert(app.watcher.WaitFor(events, scaleTimeout, f), c.IsNil)
		for _, e := range actual {
			t.Assert(scale, OutputContains, fmt.Sprintf("==> %s %s %s", e.Type, e.ID, e.State))
		}
	}

	scale := app.flynn("scale", "echoer=1")
	t.Assert(scale, Succeeds)
	t.Assert(scale, SuccessfulOutputContains, "scaling echoer: 0=>1")
	t.Assert(scale, SuccessfulOutputContains, "scale completed")
	assertEventOutput(scale, ct.JobEvents{"echoer": {"up": 1}})

	scale = app.flynn("scale", "echoer=3", "printer=1")
	t.Assert(scale, Succeeds)
	t.Assert(scale, SuccessfulOutputContains, "echoer: 1=>3")
	t.Assert(scale, SuccessfulOutputContains, "printer: 0=>1")
	t.Assert(scale, SuccessfulOutputContains, "scale completed")
	assertEventOutput(scale, ct.JobEvents{"echoer": {"up": 2}, "printer": {"up": 1}})

	// no args should show current scale
	scale = app.flynn("scale")
	t.Assert(scale, Succeeds)
	t.Assert(scale, SuccessfulOutputContains, "echoer=3")
	t.Assert(scale, SuccessfulOutputContains, "printer=1")
	t.Assert(scale, SuccessfulOutputContains, "crasher=0")
	t.Assert(scale, SuccessfulOutputContains, "omni=0")

	// scale should only affect specified processes
	scale = app.flynn("scale", "printer=2")
	t.Assert(scale, Succeeds)
	t.Assert(scale, SuccessfulOutputContains, "printer: 1=>2")
	t.Assert(scale, SuccessfulOutputContains, "scale completed")
	assertEventOutput(scale, ct.JobEvents{"printer": {"up": 1}})
	scale = app.flynn("scale")
	t.Assert(scale, Succeeds)
	t.Assert(scale, SuccessfulOutputContains, "echoer=3")
	t.Assert(scale, SuccessfulOutputContains, "printer=2")
	t.Assert(scale, SuccessfulOutputContains, "crasher=0")
	t.Assert(scale, SuccessfulOutputContains, "omni=0")

	// unchanged processes shouldn't appear in output
	scale = app.flynn("scale", "echoer=3", "printer=0")
	t.Assert(scale, Succeeds)
	t.Assert(scale, SuccessfulOutputContains, "printer: 2=>0")
	t.Assert(scale, c.Not(OutputContains), "echoer")
	t.Assert(scale, SuccessfulOutputContains, "scale completed")
	assertEventOutput(scale, ct.JobEvents{"printer": {"down": 2}})

	// --no-wait should not wait for scaling to complete
	scale = app.flynn("scale", "--no-wait", "echoer=0")
	t.Assert(scale, Succeeds)
	t.Assert(scale, SuccessfulOutputContains, "scaling echoer: 3=>0")
	t.Assert(scale, c.Not(OutputContains), "scale completed")
}

func (s *CLISuite) TestRun(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()

	// this shouldn't be logged
	t.Assert(app.sh("echo foo"), Outputs, "foo\n")
	// drain the events
	app.waitFor(ct.JobEvents{"": {"up": 1, "down": 1}})

	// this should be logged due to the --enable-log flag
	t.Assert(app.flynn("run", "--enable-log", "echo", "hello"), Outputs, "hello\n")
	app.waitFor(ct.JobEvents{"": {"up": 1, "down": 1}})

	detached := app.flynn("run", "-d", "echo", "world")
	t.Assert(detached, Succeeds)
	t.Assert(detached, c.Not(Outputs), "world\n")

	id := strings.TrimSpace(detached.Output)
	jobID := app.waitFor(ct.JobEvents{"": {"up": 1, "down": 1}})
	t.Assert(jobID, c.Equals, id)
	t.Assert(app.flynn("log", "--raw-output"), Outputs, "hello\nworld\n")

	// test stdin and stderr
	streams := app.flynnCmd("run", "sh", "-c", "cat 1>&2")
	stdin, err := streams.StdinPipe()
	t.Assert(err, c.IsNil)
	go func() {
		stdin.Write([]byte("goto stderr"))
		stdin.Close()
	}()
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	streams.Stderr = &stderr
	streams.Stdout = &stdout
	t.Assert(streams.Run(), c.IsNil)
	t.Assert(stderr.String(), c.Equals, "goto stderr")
	t.Assert(stdout.String(), c.Equals, "")

	// test exit code
	exit := app.sh("exit 42")
	t.Assert(exit, c.Not(Succeeds))
	if msg, ok := exit.Err.(*exec.ExitError); ok { // there is error code
		code := msg.Sys().(syscall.WaitStatus).ExitStatus()
		t.Assert(code, c.Equals, 42)
	} else {
		t.Fatal("There was no error code!")
	}
}

func (s *CLISuite) TestRunSignal(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	wait := func(cmd *exec.Cmd) error {
		done := make(chan error)
		go func() {
			done <- cmd.Wait()
		}()
		select {
		case err := <-done:
			return err
		case <-time.After(12 * time.Second):
			return errors.New("timeout")
		}
	}
	var out bytes.Buffer
	cmd := app.flynnCmd("run", "/bin/signal")
	cmd.Stdout = &out
	t.Assert(cmd.Start(), c.IsNil)
	app.waitForService("signal-service")
	t.Assert(cmd.Process.Signal(syscall.SIGINT), c.IsNil)
	t.Assert(wait(cmd), c.IsNil)
	t.Assert(out.String(), c.Equals, "got signal: interrupt")
}

func (s *CLISuite) TestEnv(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	t.Assert(app.flynn("env", "set", "ENV_TEST=var", "SECOND_VAL=2"), Succeeds)
	t.Assert(app.flynn("env"), SuccessfulOutputContains, "ENV_TEST=var\nSECOND_VAL=2")
	t.Assert(app.flynn("env", "get", "ENV_TEST"), Outputs, "var\n")
	// test that containers do contain the ENV var
	t.Assert(app.sh("echo $ENV_TEST"), Outputs, "var\n")
	t.Assert(app.flynn("env", "unset", "ENV_TEST"), Succeeds)
	t.Assert(app.sh("echo $ENV_TEST"), Outputs, "\n")
}

func (s *CLISuite) TestMeta(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	t.Assert(app.flynn("meta", "set", "META_TEST=var", "SECOND_VAL=2"), Succeeds)
	t.Assert(app.flynn("meta").Output, Matches, `META_TEST *var`)
	t.Assert(app.flynn("meta").Output, Matches, `SECOND_VAL *2`)
	// test that unset can remove all meta tags
	t.Assert(app.flynn("meta", "unset", "META_TEST", "SECOND_VAL"), Succeeds)
	t.Assert(app.flynn("meta").Output, c.Not(Matches), `META_TEST *var`)
	t.Assert(app.flynn("meta").Output, c.Not(Matches), `SECOND_VAL *2`)
}

func (s *CLISuite) TestKill(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	t.Assert(app.flynn("scale", "--no-wait", "echoer=1"), Succeeds)
	jobID := app.waitFor(ct.JobEvents{"echoer": {"up": 1}})

	t.Assert(app.flynn("kill", jobID), Succeeds)
	stoppedID := app.waitFor(ct.JobEvents{"echoer": {"down": 1}})
	t.Assert(stoppedID, c.Equals, jobID)
}

func (s *CLISuite) TestRoute(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()

	// The router API does not currently give us a "read your own writes"
	// guarantee, so we must retry a few times if we don't get the expected
	// result.
	assertRouteContains := func(str string, contained bool) {
		var res *CmdResult
		attempt.Strategy{
			Total: 10 * time.Second,
			Delay: 500 * time.Millisecond,
		}.Run(func() error {
			res = app.flynn("route")
			if contained == strings.Contains(res.Output, str) {
				return nil
			}
			return errors.New("unexpected output")
		})
		if contained {
			t.Assert(res, SuccessfulOutputContains, str)
		} else {
			t.Assert(res, c.Not(SuccessfulOutputContains), str)
		}
	}

	// flynn route add http
	route := random.String(32) + ".dev"
	newRoute := app.flynn("route", "add", "http", "--sticky", route)
	t.Assert(newRoute, Succeeds)
	routeID := strings.TrimSpace(newRoute.Output)
	assertRouteContains(routeID, true)

	// ensure sticky flag is set
	routes, err := s.controllerClient(t).RouteList(app.name)
	t.Assert(err, c.IsNil)
	var found bool
	for _, r := range routes {
		if fmt.Sprintf("%s/%s", r.Type, r.ID) != routeID {
			continue
		}
		t.Assert(r.Sticky, c.Equals, true)
		found = true
	}
	t.Assert(found, c.Equals, true, c.Commentf("didn't find route"))

	// flynn route remove
	t.Assert(app.flynn("route", "remove", routeID), Succeeds)
	assertRouteContains(routeID, false)

	// flynn route add tcp
	tcpRoute := app.flynn("route", "add", "tcp")
	t.Assert(tcpRoute, Succeeds)
	routeID = strings.Split(tcpRoute.Output, " ")[0]
	assertRouteContains(routeID, true)

	// flynn route remove
	t.Assert(app.flynn("route", "remove", routeID), Succeeds)
	assertRouteContains(routeID, false)
}

func (s *CLISuite) TestProvider(t *c.C) {
	t.Assert(s.flynn(t, "provider"), SuccessfulOutputContains, "postgres")
}

func (s *CLISuite) TestResource(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	t.Assert(app.flynn("resource", "add", "postgres").Output, Matches, `Created resource \w+ and release \w+.`)

	res, err := s.controllerClient(t).AppResourceList(app.name)
	t.Assert(err, c.IsNil)
	t.Assert(res, c.HasLen, 1)
	// the env variables should be set
	t.Assert(app.sh("test -n $FLYNN_POSTGRES"), Succeeds)
	t.Assert(app.sh("test -n $PGUSER"), Succeeds)
	t.Assert(app.sh("test -n $PGPASSWORD"), Succeeds)
	t.Assert(app.sh("test -n $PGDATABASE"), Succeeds)
}

func (s *CLISuite) TestResourceList(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	t.Assert(app.flynn("resource", "add", "postgres"), Succeeds)
	t.Assert(app.flynn("resource").Output, Matches, `postgres`)
}

func (s *CLISuite) TestLog(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	t.Assert(app.flynn("run", "-d", "echo", "hello", "world"), Succeeds)
	app.waitFor(ct.JobEvents{"": {"up": 1, "down": 1}})
	t.Assert(app.flynn("log", "--raw-output"), Outputs, "hello world\n")
}

func (s *CLISuite) TestLogFilter(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	for i := 0; i < 2; i++ {
		t.Assert(app.flynn("scale", "crasher=2"), Succeeds)
		t.Assert(app.flynn("scale", "crasher=0"), Succeeds)
	}
	t.Assert(app.flynn("run", "-d", "echo", "hello", "world"), Succeeds)
	jobID := app.waitFor(ct.JobEvents{"": {"up": 1, "down": 1}})

	tests := []struct {
		args     []string
		expected string
	}{
		{
			args:     []string{"-j", jobID, "--raw-output"},
			expected: "hello world\n",
		},
		{
			args:     []string{"-t", "", "--raw-output"},
			expected: "hello world\n",
		},
		{
			args:     []string{"-t", "crasher", "--raw-output", "-n", "1"},
			expected: "I like to crash\n",
		},
	}

	for _, test := range tests {
		args := append([]string{"log"}, test.args...)
		t.Assert(app.flynn(args...), Outputs, test.expected)
	}
}

func (s *CLISuite) TestLogStderr(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	t.Assert(app.flynn("run", "-d", "sh", "-c", "echo hello && echo world >&2"), Succeeds)
	app.waitFor(ct.JobEvents{"": {"up": 1, "down": 1}})
	runLog := func(split bool) (stdout, stderr bytes.Buffer) {
		args := []string{"log", "--raw-output"}
		if split {
			args = append(args, "--split-stderr")
		}
		args = append(args)
		log := app.flynnCmd(args...)
		log.Stdout = &stdout
		log.Stderr = &stderr
		t.Assert(log.Run(), c.IsNil, c.Commentf("STDERR = %q", stderr.String()))
		return
	}
	stdout, stderr := runLog(false)
	// non-deterministic order
	t.Assert(stdout.String(), Matches, "hello")
	t.Assert(stdout.String(), Matches, "world")
	t.Assert(stderr.String(), c.Equals, "")
	stdout, stderr = runLog(true)
	t.Assert(stdout.String(), c.Equals, "hello\n")
	t.Assert(stderr.String(), c.Equals, "world\n")
}

func (s *CLISuite) TestLogFollow(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()

	t.Assert(app.flynn("run", "-d", "sh", "-c", "sleep 2 && for i in 1 2 3 4 5; do echo \"line $i\"; done"), Succeeds)
	app.waitFor(ct.JobEvents{"": {"up": 1}})

	log := app.flynnCmd("log", "--raw-output", "--follow")
	logStdout, err := log.StdoutPipe()
	t.Assert(err, c.IsNil)
	t.Assert(log.Start(), c.IsNil)
	defer log.Process.Kill()

	// use a goroutine + channel so we can timeout the stdout read
	type line struct {
		text string
		err  error
	}
	lines := make(chan line)
	go func() {
		buf := bufio.NewReader(logStdout)
		for {
			text, err := buf.ReadBytes('\n')
			if err != nil {
				if err != io.EOF {
					lines <- line{"", err}
				}
				break
			}
			lines <- line{string(text), nil}
		}
	}()
	readline := func() (string, error) {
		select {
		case l := <-lines:
			if l.err != nil {
				return "", fmt.Errorf("could not read log output: %s", l.err)
			}
			return l.text, nil
		case <-time.After(5 * time.Second):
			return "", errors.New("timed out waiting for log output")
		}
	}
	var stderr bytes.Buffer
	for i := 1; i < 6; i++ {
		expected := fmt.Sprintf("line %d\n", i)
		actual, err := readline()
		if err != nil {
			t.Logf("STDERR = %q", stderr.String())
		}
		t.Assert(err, c.IsNil)
		t.Assert(actual, c.Equals, expected)
	}
}

func (s *CLISuite) TestCluster(t *c.C) {
	// use a custom flynnrc to avoid disrupting other tests
	file, err := ioutil.TempFile("", "")
	t.Assert(err, c.IsNil)
	flynn := func(cmdArgs ...string) *CmdResult {
		cmd := exec.Command(args.CLI, cmdArgs...)
		cmd.Env = flynnEnv(file.Name())
		return run(t, cmd)
	}

	// cluster add
	t.Assert(flynn("cluster", "add", "-g", "test.example.com:2222", "-p", "KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs=", "test", "https://controller.test.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster"), SuccessfulOutputContains, "test")
	t.Assert(flynn("cluster", "add", "-f", "-g", "test.example.com:2222", "-p", "KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs=", "test", "https://controller.test.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster"), SuccessfulOutputContains, "test")
	t.Assert(flynn("cluster", "add", "-f", "-d", "-g", "test.example.com:2222", "-p", "KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs=", "test", "https://controller.test.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster"), SuccessfulOutputContains, "test")
	// make sure the cluster is present in the config
	cfg, err := config.ReadFile(file.Name())
	t.Assert(err, c.IsNil)
	t.Assert(cfg.Default, c.Equals, "test")
	t.Assert(cfg.Clusters, c.HasLen, 1)
	t.Assert(cfg.Clusters[0].Name, c.Equals, "test")
	// overwriting (without --force) should not work
	t.Assert(flynn("cluster", "add", "test", "foo", "bar"), c.Not(Succeeds))
	t.Assert(flynn("cluster"), SuccessfulOutputContains, "test")
	t.Assert(flynn("cluster"), SuccessfulOutputContains, "(default)")
	// change default cluster
	t.Assert(flynn("cluster", "default", "test"), SuccessfulOutputContains, "\"test\" is now the default cluster.")
	t.Assert(flynn("cluster", "default", "missing"), OutputContains, "Cluster \"missing\" does not exist and cannot be set as default.")
	t.Assert(flynn("cluster", "default"), SuccessfulOutputContains, "test")
	cfg, err = config.ReadFile(file.Name())
	t.Assert(err, c.IsNil)
	t.Assert(cfg.Default, c.Equals, "test")
	// cluster remove
	t.Assert(flynn("cluster", "remove", "test"), Succeeds)
	t.Assert(flynn("cluster"), c.Not(SuccessfulOutputContains), "test")
	cfg, err = config.ReadFile(file.Name())
	t.Assert(err, c.IsNil)
	t.Assert(cfg.Clusters, c.HasLen, 0)
	// cluster remove default and set next available
	t.Assert(flynn("cluster", "add", "-d", "-g", "test.example.com:2222", "-p", "KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs=", "test", "https://controller.test.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster", "add", "-g", "next.example.com:2222", "-p", "KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs=", "next", "https://controller.next.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster", "remove", "test"), SuccessfulOutputContains, "Cluster \"test\" removed and \"next\" is now the default cluster.")
	t.Assert(flynn("cluster", "default"), SuccessfulOutputContains, "next")
}

func (s *CLISuite) TestRelease(t *c.C) {
	releaseJSON := []byte(`{
		"env": {"GLOBAL": "FOO"},
		"processes": {
			"echoer": {
				"cmd": ["/bin/echoer"],
				"env": {"ECHOER_ONLY": "BAR"}
			},
			"env": {
				"cmd": ["sh", "-c", "env; while true; do sleep 60; done"],
				"env": {"ENV_ONLY": "BAZ"}
			}
		}
	}`)
	release := &ct.Release{}
	t.Assert(json.Unmarshal(releaseJSON, &release), c.IsNil)
	for typ, proc := range release.Processes {
		resource.SetDefaults(&proc.Resources)
		release.Processes[typ] = proc
	}

	file, err := ioutil.TempFile("", "")
	t.Assert(err, c.IsNil)
	file.Write(releaseJSON)
	file.Close()

	app := s.newCliTestApp(t)
	defer app.cleanup()
	t.Assert(app.flynn("release", "add", "-f", file.Name(), imageURIs["test-apps"]), Succeeds)

	r, err := s.controller.GetAppRelease(app.name)
	t.Assert(err, c.IsNil)
	t.Assert(r.Env, c.DeepEquals, release.Env)
	t.Assert(r.Processes, c.DeepEquals, release.Processes)

	scaleCmd := app.flynn("scale", "--no-wait", "env=1", "foo=1")
	t.Assert(scaleCmd, c.Not(Succeeds))
	t.Assert(scaleCmd, OutputContains, "ERROR: unknown process types: \"foo\"")

	// create a job watcher for the new release
	watcher, err := s.controllerClient(t).WatchJobEvents(app.name, r.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	scaleCmd = app.flynn("scale", "--no-wait", "env=1")
	t.Assert(watcher.WaitFor(ct.JobEvents{"env": {"up": 1}}, scaleTimeout, nil), c.IsNil)
	envLog := app.flynn("log")
	t.Assert(envLog, Succeeds)
	t.Assert(envLog, SuccessfulOutputContains, "GLOBAL=FOO")
	t.Assert(envLog, SuccessfulOutputContains, "ENV_ONLY=BAZ")
	t.Assert(envLog, c.Not(SuccessfulOutputContains), "ECHOER_ONLY=BAR")
}

func (s *CLISuite) TestLimits(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	t.Assert(app.flynn("limit", "set", "resources", "memory=512MB", "max_fd=12k"), Succeeds)

	release, err := s.controller.GetAppRelease(app.name)
	t.Assert(err, c.IsNil)
	proc, ok := release.Processes["resources"]
	if !ok {
		t.Fatal("missing resources process type")
	}
	r := proc.Resources
	t.Assert(*r[resource.TypeMemory].Limit, c.Equals, int64(536870912))
	t.Assert(*r[resource.TypeMaxFD].Limit, c.Equals, int64(12000))

	cmd := app.flynn("limit", "-t", "resources")
	t.Assert(cmd, Succeeds)
	t.Assert(cmd, OutputContains, "memory=512MB")
	t.Assert(cmd, OutputContains, "max_fd=12000")
}

func (s *CLISuite) TestRunLimits(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	cmd := app.flynn("run", "sh", "-c", resourceCmd)
	t.Assert(cmd, Succeeds)
	defaults := resource.Defaults()
	limits := strings.Split(strings.TrimSpace(cmd.Output), "\n")
	t.Assert(limits, c.HasLen, 2)
	t.Assert(limits[0], c.Equals, strconv.FormatInt(*defaults[resource.TypeMemory].Limit, 10))
	t.Assert(limits[1], c.Equals, strconv.FormatInt(*defaults[resource.TypeMaxFD].Limit, 10))
}

func (s *CLISuite) TestExportImport(t *c.C) {
	srcApp := "app-export" + random.String(8)
	dstApp := "app-import" + random.String(8)

	// create and push app+db
	r := s.newGitRepo(t, "http")
	t.Assert(r.flynn("create", srcApp), Succeeds)
	t.Assert(r.flynn("key", "add", r.ssh.Pub), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)
	t.Assert(r.flynn("resource", "add", "postgres"), Succeeds)
	t.Assert(r.flynn("pg", "psql", "--", "-c",
		"CREATE table foos (data text); INSERT INTO foos (data) VALUES ('foobar')"), Succeeds)

	// export app
	file := filepath.Join(t.MkDir(), "export.tar")
	t.Assert(r.flynn("export", "-f", file), Succeeds)

	// remove db table from source app
	t.Assert(r.flynn("pg", "psql", "--", "-c", "DROP TABLE foos"), Succeeds)

	// import app
	t.Assert(r.flynn("import", "--name", dstApp, "--file", file), Succeeds)

	// test db was imported
	query := r.flynn("-a", dstApp, "pg", "psql", "--", "-c", "SELECT * FROM foos")
	t.Assert(query, SuccessfulOutputContains, "foobar")

	// wait for it to start
	_, err := s.discoverdClient(t).Instances(dstApp+"-web", 10*time.Second)
	t.Assert(err, c.IsNil)
}
