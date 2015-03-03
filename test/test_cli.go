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
	"strings"
	"syscall"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/flynn/flynn/cli/config"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/stream"
)

type CLISuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&CLISuite{})

func (s *CLISuite) flynn(t *c.C, args ...string) *CmdResult {
	return flynn(t, "/", args...)
}

func (s *CLISuite) newCliTestApp(t *c.C) *cliTestApp {
	app, _ := s.createApp(t)
	events := make(chan *ct.JobEvent)
	stream, err := s.controllerClient(t).StreamJobEvents(app.Name, 0, events)
	t.Assert(err, c.IsNil)
	return &cliTestApp{
		name:   app.Name,
		stream: stream,
		events: events,
		disc:   s.discoverdClient(t),
		t:      t,
	}
}

type cliTestApp struct {
	name   string
	stream stream.Stream
	events chan *ct.JobEvent
	disc   *discoverd.Client
	t      *c.C
}

func (a *cliTestApp) flynn(args ...string) *CmdResult {
	return flynn(a.t, "/", append([]string{"-a", a.name}, args...)...)
}

func (a *cliTestApp) flynnCmd(args ...string) *exec.Cmd {
	return flynnCmd("/", append([]string{"-a", a.name}, args...)...)
}

func (a *cliTestApp) waitFor(events jobEvents) (int64, string) {
	return waitForJobEvents(a.t, a.stream, a.events, events)
}

func (a *cliTestApp) waitForService(name string) {
	_, err := a.disc.Instances(name, 30*time.Second)
	a.t.Assert(err, c.IsNil)
}

func (a *cliTestApp) sh(cmd string) *CmdResult {
	return a.flynn("run", "sh", "-c", cmd)
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
	t.Assert(app.flynn("apps"), OutputContains, name)
	t.Assert(app.flynn("-c", "default", "apps"), OutputContains, name)
	if remote == "" {
		t.Assert(app.git("remote", "-v"), c.Not(OutputContains), flynnRemote)
	} else {
		t.Assert(app.git("remote", "-v"), OutputContains, flynnRemote)
	}

	// make sure flynn components are listed
	t.Assert(app.flynn("apps"), OutputContains, "router")
	t.Assert(app.flynn("-c", "default", "apps"), OutputContains, "router")

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
	t.Assert(app.git("remote", "-v"), c.Not(OutputContains), flynnRemote)
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

	t.Assert(app.flynn("key"), OutputContains, fingerprint)

	t.Assert(app.git("commit", "--allow-empty", "-m", "should succeed"), Succeeds)
	t.Assert(app.git("push", "flynn", "master"), Succeeds)

	t.Assert(app.flynn("key", "remove", fingerprint), Succeeds)
	t.Assert(app.flynn("key"), c.Not(OutputContains), fingerprint)

	t.Assert(app.git("commit", "--allow-empty", "-m", "should fail"), Succeeds)
	t.Assert(app.git("push", "flynn", "master"), c.Not(Succeeds))

	t.Assert(app.flynn("delete", "--yes"), Succeeds)
}

func (s *CLISuite) TestPs(t *c.C) {
	app := s.newCliTestApp(t)
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

	scale := app.flynn("scale", "echoer=1")
	_, jobID := app.waitFor(jobEvents{"echoer": {"up": 1}})
	t.Assert(scale, Succeeds)
	t.Assert(scale, OutputContains, "scaling echoer: 0=>1")
	t.Assert(scale, OutputContains, fmt.Sprintf("==> echoer %s up", jobID))
	t.Assert(scale, OutputContains, "scale completed")

	scale = app.flynn("scale", "echoer=3", "printer=1")
	t.Assert(scale, Succeeds)
	t.Assert(scale, OutputContains, "echoer: 1=>3")
	t.Assert(scale, OutputContains, "printer: 0=>1")
	t.Assert(scale, OutputContains, "scale completed")

	// no args should show current scale
	scale = app.flynn("scale")
	t.Assert(scale, Succeeds)
	t.Assert(scale, OutputContains, "echoer=3")
	t.Assert(scale, OutputContains, "printer=1")
	t.Assert(scale, OutputContains, "crasher=0")
	t.Assert(scale, OutputContains, "omni=0")

	// scale should only affect specified processes
	scale = app.flynn("scale", "printer=2")
	t.Assert(scale, Succeeds)
	t.Assert(scale, OutputContains, "printer: 1=>2")
	t.Assert(scale, OutputContains, "scale completed")
	scale = app.flynn("scale")
	t.Assert(scale, Succeeds)
	t.Assert(scale, OutputContains, "echoer=3")
	t.Assert(scale, OutputContains, "printer=2")
	t.Assert(scale, OutputContains, "crasher=0")
	t.Assert(scale, OutputContains, "omni=0")

	// unchanged processes shouldn't appear in output
	scale = app.flynn("scale", "echoer=3", "printer=0")
	t.Assert(scale, Succeeds)
	t.Assert(scale, OutputContains, "printer: 2=>0")
	t.Assert(scale, c.Not(OutputContains), "echoer")
	t.Assert(scale, OutputContains, "scale completed")

	// --no-wait should not wait for scaling to complete
	scale = app.flynn("scale", "--no-wait", "echoer=0")
	t.Assert(scale, Succeeds)
	t.Assert(scale, OutputContains, "scaling echoer: 3=>0")
	t.Assert(scale, c.Not(OutputContains), "scale completed")
	app.waitFor(jobEvents{"echoer": {"down": 3}})
}

func (s *CLISuite) TestRun(t *c.C) {
	app := s.newCliTestApp(t)

	// this still goes to the log stream because there's no TTY:
	t.Assert(app.sh("echo hello"), Outputs, "hello\n")
	// drain the events
	app.waitFor(jobEvents{"": {"up": 1, "down": 1}})

	detached := app.flynn("run", "-d", "echo", "world")
	t.Assert(detached, Succeeds)
	t.Assert(detached, c.Not(Outputs), "world\n")

	id := strings.TrimSpace(detached.Output)
	_, jobID := app.waitFor(jobEvents{"": {"up": 1, "down": 1}})
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
	t.Assert(app.flynn("env", "set", "ENV_TEST=var", "SECOND_VAL=2"), Succeeds)
	t.Assert(app.flynn("env"), OutputContains, "ENV_TEST=var\nSECOND_VAL=2")
	t.Assert(app.flynn("env", "get", "ENV_TEST"), Outputs, "var\n")
	// test that containers do contain the ENV var
	t.Assert(app.sh("echo $ENV_TEST"), Outputs, "var\n")
	t.Assert(app.flynn("env", "unset", "ENV_TEST"), Succeeds)
	t.Assert(app.sh("echo $ENV_TEST"), Outputs, "\n")
}

func (s *CLISuite) TestKill(t *c.C) {
	app := s.newCliTestApp(t)
	t.Assert(app.flynn("scale", "--no-wait", "echoer=1"), Succeeds)
	_, jobID := app.waitFor(jobEvents{"echoer": {"up": 1}})

	t.Assert(app.flynn("kill", jobID), Succeeds)
	_, stoppedID := app.waitFor(jobEvents{"echoer": {"down": 1}})
	t.Assert(stoppedID, c.Equals, jobID)
}

func (s *CLISuite) TestRoute(t *c.C) {
	app := s.newCliTestApp(t)

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
			t.Assert(res, OutputContains, str)
		} else {
			t.Assert(res, c.Not(OutputContains), str)
		}
	}

	// flynn route add http
	route := random.String(32) + ".dev"
	newRoute := app.flynn("route", "add", "http", route)
	t.Assert(newRoute, Succeeds)
	routeID := strings.TrimSpace(newRoute.Output)
	assertRouteContains(routeID, true)

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
	t.Assert(s.flynn(t, "provider"), OutputContains, "postgres")
}

func (s *CLISuite) TestResource(t *c.C) {
	app := s.newCliTestApp(t)
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
	t.Assert(app.flynn("resource", "add", "postgres"), Succeeds)
	t.Assert(app.flynn("resource").Output, Matches, `postgres`)
}

func (s *CLISuite) TestLog(t *c.C) {
	app := s.newCliTestApp(t)
	t.Assert(app.flynn("run", "-d", "echo", "hello", "world"), Succeeds)
	app.waitFor(jobEvents{"": {"up": 1, "down": 1}})
	t.Assert(app.flynn("log", "--raw-output"), Outputs, "hello world\n")
}

func (s *CLISuite) TestLogStderr(t *c.C) {
	app := s.newCliTestApp(t)
	t.Assert(app.flynn("run", "-d", "sh", "-c", "echo hello && echo world >&2"), Succeeds)
	app.waitFor(jobEvents{"": {"up": 1, "down": 1}})
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

	var stderr bytes.Buffer
	t.Assert(app.flynn("run", "-d", "sh", "-c", "sleep 2 && for i in 1 2 3 4 5; do echo \"line $i\"; done"), Succeeds)
	app.waitFor(jobEvents{"": {"starting": 1}})

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
	t.Assert(flynn("cluster"), OutputContains, "test")
	t.Assert(flynn("cluster", "add", "-f", "-g", "test.example.com:2222", "-p", "KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs=", "test", "https://controller.test.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster"), OutputContains, "test")
	t.Assert(flynn("cluster", "add", "-f", "-d", "-g", "test.example.com:2222", "-p", "KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs=", "test", "https://controller.test.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster"), OutputContains, "test")
	// make sure the cluster is present in the config
	cfg, err := config.ReadFile(file.Name())
	t.Assert(err, c.IsNil)
	t.Assert(cfg.Default, c.Equals, "test")
	t.Assert(cfg.Clusters, c.HasLen, 1)
	t.Assert(cfg.Clusters[0].Name, c.Equals, "test")
	// overwriting (without --force) should not work
	t.Assert(flynn("cluster", "add", "test", "foo", "bar"), c.Not(Succeeds))
	t.Assert(flynn("cluster"), OutputContains, "test")
	t.Assert(flynn("cluster"), OutputContains, "(default)")
	// change default cluster
	t.Assert(flynn("cluster", "default", "test"), OutputContains, "\"test\" is now the default cluster.")
	t.Assert(flynn("cluster", "default", "missing"), OutputContains, "Cluster \"missing\" not found.")
	t.Assert(flynn("cluster", "default"), OutputContains, "test")
	cfg, err = config.ReadFile(file.Name())
	t.Assert(err, c.IsNil)
	t.Assert(cfg.Default, c.Equals, "test")
	// cluster remove
	t.Assert(flynn("cluster", "remove", "test"), Succeeds)
	t.Assert(flynn("cluster"), c.Not(OutputContains), "test")
	cfg, err = config.ReadFile(file.Name())
	t.Assert(err, c.IsNil)
	t.Assert(cfg.Clusters, c.HasLen, 0)
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

	file, err := ioutil.TempFile("", "")
	t.Assert(err, c.IsNil)
	file.Write(releaseJSON)
	file.Close()

	app := s.newCliTestApp(t)
	t.Assert(app.flynn("release", "add", "-f", file.Name(), imageURIs["test-apps"]), Succeeds)

	r, err := s.controller.GetAppRelease(app.name)
	t.Assert(err, c.IsNil)
	t.Assert(r.Env, c.DeepEquals, release.Env)
	t.Assert(r.Processes, c.DeepEquals, release.Processes)

	t.Assert(app.flynn("scale", "--no-wait", "env=1"), Succeeds)
	app.waitFor(jobEvents{"env": {"up": 1}})
	envLog := app.flynn("log")
	t.Assert(envLog, Succeeds)
	t.Assert(envLog, OutputContains, "GLOBAL=FOO")
	t.Assert(envLog, OutputContains, "ENV_ONLY=BAZ")
	t.Assert(envLog, c.Not(OutputContains), "ECHOER_ONLY=BAR")
}
