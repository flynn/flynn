package main

import (
	"bufio"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	units "github.com/docker/go-units"
	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/pkg/attempt"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/tlscert"
	c "github.com/flynn/go-check"
)

type CLISuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&CLISuite{})

const UUIDRegex = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"

func (s *CLISuite) flynn(t *c.C, args ...string) *CmdResult {
	return flynn(t, "/", args...)
}

func (s *CLISuite) TestCreateAppNoGit(t *c.C) {
	dir := t.MkDir()
	name := random.String(30)
	t.Assert(flynn(t, dir, "create", name), Outputs, fmt.Sprintf("Created %s\n", name))
}

func testApp(s *CLISuite, t *c.C, remote string) {
	app := s.newGitRepo(t, "")
	name := random.String(30)
	flynnRemote := fmt.Sprintf("%s\t%s/%s.git (push)", remote, s.clusterConf(t).GitURL, name)

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
			id := e.ID
			if id == "" {
				id = e.UUID
			}
			t.Assert(scale, OutputContains, fmt.Sprintf("==> %s %s %s", e.Type, id, e.State))
		}
	}

	scale := app.flynn("scale", "echoer=1")
	t.Assert(scale, Succeeds)
	t.Assert(scale, SuccessfulOutputContains, "scaling echoer: 0=>1")
	t.Assert(scale, SuccessfulOutputContains, "scale completed")
	assertEventOutput(scale, ct.JobEvents{"echoer": {ct.JobStateUp: 1}})

	scale = app.flynn("scale", "echoer=3", "printer=1")
	t.Assert(scale, Succeeds)
	t.Assert(scale, SuccessfulOutputContains, "echoer: 1=>3")
	t.Assert(scale, SuccessfulOutputContains, "printer: 0=>1")
	t.Assert(scale, SuccessfulOutputContains, "scale completed")
	assertEventOutput(scale, ct.JobEvents{"echoer": {ct.JobStateUp: 2}, "printer": {ct.JobStateUp: 1}})

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
	t.Assert(scale, c.Not(OutputContains), "echoer")
	t.Assert(scale, SuccessfulOutputContains, "scale completed")
	assertEventOutput(scale, ct.JobEvents{"printer": {ct.JobStateUp: 1}})
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
	assertEventOutput(scale, ct.JobEvents{"printer": {ct.JobStateDown: 2}})

	// --no-wait should not wait for scaling to complete
	scale = app.flynn("scale", "--no-wait", "echoer=0")
	t.Assert(scale, Succeeds)
	t.Assert(scale, SuccessfulOutputContains, "scaling echoer: 3=>0")
	t.Assert(scale, c.Not(OutputContains), "scale completed")
}

func (s *CLISuite) TestScaleAll(t *c.C) {
	client := s.controllerClient(t)
	app := s.newCliTestApp(t)
	release := app.release
	defer app.cleanup()

	scale := app.flynn("scale", "echoer=1", "printer=2")
	t.Assert(scale, Succeeds)

	scale = app.flynn("scale", "--all")
	t.Assert(scale, Succeeds)
	t.Assert(scale, SuccessfulOutputContains, fmt.Sprintf("%s (current)\n", release.ID))
	t.Assert(scale, SuccessfulOutputContains, "echoer=1")
	t.Assert(scale, SuccessfulOutputContains, "printer=2")

	prevRelease := release
	release = &ct.Release{
		ArtifactIDs: release.ArtifactIDs,
		Env:         release.Env,
		Meta:        release.Meta,
		Processes:   release.Processes,
	}
	t.Assert(client.CreateRelease(app.id, release), c.IsNil)
	t.Assert(client.SetAppRelease(app.id, release.ID), c.IsNil)

	scale = app.flynn("scale", "echoer=2", "printer=1")
	t.Assert(scale, Succeeds)

	scale = app.flynn("scale", "--all")
	t.Assert(scale, Succeeds)
	t.Assert(scale, SuccessfulOutputContains, fmt.Sprintf("%s (current)\n", release.ID))
	t.Assert(scale, SuccessfulOutputContains, "echoer=2")
	t.Assert(scale, SuccessfulOutputContains, "printer=1")
	t.Assert(scale, SuccessfulOutputContains, fmt.Sprintf("%s\n", prevRelease.ID))
	t.Assert(scale, SuccessfulOutputContains, "echoer=1")
	t.Assert(scale, SuccessfulOutputContains, "printer=2")

	scale = app.flynn("scale", "--all", "--release", release.ID)
	t.Assert(scale, c.Not(Succeeds))

	scale = app.flynn("scale", "--all", "echoer=3", "printer=3")
	t.Assert(scale, c.Not(Succeeds))
}

func (s *CLISuite) TestRun(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()

	// this shouldn't be logged
	t.Assert(app.sh("echo foo"), Outputs, "foo\n")
	// drain the events
	app.waitFor(ct.JobEvents{"": {ct.JobStateDown: 1}})

	// this should be logged due to the --enable-log flag
	t.Assert(app.flynn("run", "--enable-log", "echo", "hello"), Outputs, "hello\n")
	app.waitFor(ct.JobEvents{"": {ct.JobStateDown: 1}})

	detached := app.flynn("run", "-d", "echo", "world")
	t.Assert(detached, Succeeds)
	t.Assert(detached, c.Not(Outputs), "world\n")

	id := strings.TrimSpace(detached.Output)
	jobID := app.waitFor(ct.JobEvents{"": {ct.JobStateDown: 1}})
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

func (s *CLISuite) TestRunNoImage(t *c.C) {
	r := s.newGitRepo(t, "empty-release")
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.flynn("env", "set", "FOO=BAR", "BUILDPACK_URL=https://github.com/kr/heroku-buildpack-inline"), Succeeds)

	// running a command before pushing should error
	cmd := r.flynn("run", "env")
	t.Assert(cmd, c.Not(Succeeds))
	t.Assert(cmd, OutputContains, "App release has no image, push a release first")

	// command should work after push
	t.Assert(r.git("push", "flynn", "master"), Succeeds)
	cmd = r.flynn("run", "env")
	t.Assert(cmd, Succeeds)
	t.Assert(cmd, OutputContains, "FOO=BAR")
}

func (s *CLISuite) TestEnv(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	t.Assert(app.flynn("env", "set", "ENV_TEST=var", "SECOND_VAL=2"), Succeeds)
	t.Assert(app.flynn("env"), SuccessfulOutputContains, "ENV_TEST=var\nSECOND_VAL=2")
	t.Assert(app.flynn("env", "get", "ENV_TEST"), Outputs, "var\n")
	t.Assert(app.flynn("env", "set", "-t", "ping", "ENV_TEST=foo"), Succeeds)
	t.Assert(app.flynn("env", "get", "-t", "ping", "ENV_TEST"), Outputs, "foo\n")
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
	jobID := app.waitFor(ct.JobEvents{"echoer": {ct.JobStateUp: 1}})

	t.Assert(app.flynn("kill", jobID), Succeeds)
	stoppedID := app.waitFor(ct.JobEvents{"echoer": {ct.JobStateDown: 1}})
	t.Assert(stoppedID, c.Equals, jobID)
}

func (s *CLISuite) TestRoute(t *c.C) {
	client := s.controllerClient(t)
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
	newRoute := app.flynn("route", "add", "http", route)
	t.Assert(newRoute, Succeeds)
	routeID := strings.TrimSpace(newRoute.Output)
	assertRouteContains(routeID, true)

	// ensure sticky and leader flags default to not set
	routes, err := client.RouteList(app.name)
	t.Assert(err, c.IsNil)
	var found bool
	for _, r := range routes {
		if fmt.Sprintf("%s/%s", r.Type, r.ID) != routeID {
			continue
		}
		t.Assert(r.Sticky, c.Equals, false)
		t.Assert(r.Leader, c.Equals, false)
		found = true
		break
	}
	t.Assert(found, c.Equals, true, c.Commentf("didn't find route"))

	// flynn route add http --sticky --leader
	route = random.String(32) + ".dev"
	newRoute = app.flynn("route", "add", "http", "--sticky", route, "--leader")
	t.Assert(newRoute, Succeeds)
	routeID = strings.TrimSpace(newRoute.Output)
	assertRouteContains(routeID, true)

	// duplicate http route
	dupRoute := app.flynn("route", "add", "http", "--sticky", route)
	t.Assert(dupRoute, c.Not(Succeeds))
	t.Assert(dupRoute.Output, c.Equals, "conflict: Duplicate route\n")

	// ensure sticky and leader flags are set
	routes, err = client.RouteList(app.name)
	t.Assert(err, c.IsNil)
	for _, r := range routes {
		if fmt.Sprintf("%s/%s", r.Type, r.ID) != routeID {
			continue
		}
		t.Assert(r.Sticky, c.Equals, true)
		t.Assert(r.Leader, c.Equals, true)
		found = true
		break
	}
	t.Assert(found, c.Equals, true, c.Commentf("didn't find route"))

	// flynn route update --no-sticky
	newRoute = app.flynn("route", "update", routeID, "--no-sticky")
	t.Assert(newRoute, Succeeds)
	r, err := client.GetRoute(app.id, routeID)
	t.Assert(err, c.IsNil)
	t.Assert(r.Sticky, c.Equals, false)

	// flynn route update --no-leader
	newRoute = app.flynn("route", "update", routeID, "--no-leader")
	t.Assert(newRoute, Succeeds)
	r, err = client.GetRoute(app.id, routeID)
	t.Assert(err, c.IsNil)
	t.Assert(r.Leader, c.Equals, false)

	// flynn route update --service
	newRoute = app.flynn("route", "update", routeID, "--service", "foo")
	t.Assert(newRoute, Succeeds)
	r, err = client.GetRoute(app.id, routeID)
	t.Assert(err, c.IsNil)
	t.Assert(r.Service, c.Equals, "foo")
	t.Assert(r.Sticky, c.Equals, false)

	// flynn route update --sticky
	newRoute = app.flynn("route", "update", routeID, "--sticky")
	t.Assert(newRoute, Succeeds)
	r, err = client.GetRoute(app.id, routeID)
	t.Assert(err, c.IsNil)
	t.Assert(r.Sticky, c.Equals, true)
	t.Assert(r.Service, c.Equals, "foo")

	// flynn route update --leader
	newRoute = app.flynn("route", "update", routeID, "--leader")
	t.Assert(newRoute, Succeeds)
	r, err = client.GetRoute(app.id, routeID)
	t.Assert(err, c.IsNil)
	t.Assert(r.Leader, c.Equals, true)
	t.Assert(r.Service, c.Equals, "foo")

	// flynn route add domain path
	pathRoute := app.flynn("route", "add", "http", route+"/path/")
	t.Assert(pathRoute, Succeeds)
	pathRouteID := strings.TrimSpace(pathRoute.Output)
	assertRouteContains(pathRouteID, true)

	// flynn route add domain path duplicate
	dupRoute = app.flynn("route", "add", "http", route+"/path/")
	t.Assert(dupRoute, c.Not(Succeeds))
	t.Assert(dupRoute.Output, c.Equals, "conflict: Duplicate route\n")

	// flynn route add domain path without trailing should correct to trailing
	noTrailingRoute := app.flynn("route", "add", "http", route+"/path2")
	t.Assert(noTrailingRoute, Succeeds)
	noTrailingRouteID := strings.TrimSpace(noTrailingRoute.Output)
	assertRouteContains(noTrailingRouteID, true)
	// flynn route should show the corrected trailing path
	assertRouteContains("/path2/", true)

	// flynn route remove should fail because of dependent route
	delFail := app.flynn("route", "remove", routeID)
	t.Assert(delFail, c.Not(Succeeds))

	// But removing the dependent route and then the default route should work
	t.Assert(app.flynn("route", "remove", pathRouteID), Succeeds)
	assertRouteContains(pathRouteID, false)
	t.Assert(app.flynn("route", "remove", noTrailingRouteID), Succeeds)
	assertRouteContains(noTrailingRouteID, false)
	t.Assert(app.flynn("route", "remove", routeID), Succeeds)
	assertRouteContains(routeID, false)

	// flynn route add tcp
	tcpRoute := app.flynn("route", "add", "tcp")
	t.Assert(tcpRoute, Succeeds)
	routeID = strings.Split(tcpRoute.Output, " ")[0]
	assertRouteContains(routeID, true)

	// flynn route add tcp --port
	portRoute := app.flynn("route", "add", "tcp", "--port", "9999")
	t.Assert(portRoute, Succeeds)
	routeID = strings.Split(portRoute.Output, " ")[0]
	port := strings.Split(portRoute.Output, " ")[4]
	t.Assert(port, c.Equals, "9999\n")
	assertRouteContains(routeID, true)

	// flynn route update --service
	portRoute = app.flynn("route", "update", routeID, "--service", "foo")
	t.Assert(portRoute, Succeeds)
	r, err = client.GetRoute(app.id, routeID)
	t.Assert(err, c.IsNil)
	t.Assert(r.Service, c.Equals, "foo")

	// flynn route remove
	t.Assert(app.flynn("route", "remove", routeID), Succeeds)
	assertRouteContains(routeID, false)

	writeTemp := func(data, prefix string) (string, error) {
		f, err := ioutil.TempFile(os.TempDir(), fmt.Sprintf("flynn-test-%s", prefix))
		t.Assert(err, c.IsNil)
		_, err = f.WriteString(data)
		t.Assert(err, c.IsNil)
		stat, err := f.Stat()
		t.Assert(err, c.IsNil)
		return filepath.Join(os.TempDir(), stat.Name()), nil
	}

	// flynn route add http with tls cert
	cert, err := tlscert.Generate([]string{"example.com"})
	t.Assert(err, c.IsNil)
	certPath, err := writeTemp(cert.Cert, "tls-cert")
	t.Assert(err, c.IsNil)
	keyPath, err := writeTemp(cert.PrivateKey, "tls-key")
	t.Assert(err, c.IsNil)
	certRoute := app.flynn("route", "add", "http", "--tls-cert", certPath, "--tls-key", keyPath, "example.com")
	t.Assert(certRoute, Succeeds)
	routeID = strings.TrimSpace(certRoute.Output)
	r, err = client.GetRoute(app.id, routeID)
	t.Assert(err, c.IsNil)
	t.Assert(r.Domain, c.Equals, "example.com")
	t.Assert(r.Certificate, c.NotNil)
	t.Assert(r.Certificate.Cert, c.Equals, strings.Trim(cert.Cert, "\n"))
	t.Assert(r.Certificate.Key, c.Equals, strings.Trim(cert.PrivateKey, "\n"))

	// flynn route update tls cert
	cert, err = tlscert.Generate([]string{"example.com"})
	t.Assert(err, c.IsNil)
	certPath, err = writeTemp(cert.Cert, "tls-cert")
	t.Assert(err, c.IsNil)
	keyPath, err = writeTemp(cert.PrivateKey, "tls-key")
	t.Assert(err, c.IsNil)
	certRoute = app.flynn("route", "update", routeID, "--tls-cert", certPath, "--tls-key", keyPath)
	t.Assert(certRoute, Succeeds)
	r, err = client.GetRoute(app.id, routeID)
	t.Assert(err, c.IsNil)
	t.Assert(r.Domain, c.Equals, "example.com")
	t.Assert(r.Certificate, c.NotNil)
	t.Assert(r.Certificate.Cert, c.Equals, strings.Trim(cert.Cert, "\n"))
	t.Assert(r.Certificate.Key, c.Equals, strings.Trim(cert.PrivateKey, "\n"))

	// flynn route remove
	t.Assert(app.flynn("route", "remove", routeID), Succeeds)
	assertRouteContains(routeID, false)
}

func (s *CLISuite) TestProvider(t *c.C) {
	t.Assert(s.flynn(t, "provider"), SuccessfulOutputContains, "postgres")

	// flynn provider add
	testProvider := "test-provider" + random.String(8)
	testProviderUrl := "http://testprovider.discoverd"
	cmd := s.flynn(t, "provider", "add", testProvider, testProviderUrl)
	t.Assert(cmd, Outputs, fmt.Sprintf("Created provider %s.\n", testProvider))
	t.Assert(s.flynn(t, "provider"), SuccessfulOutputContains, testProvider)
}

func (s *CLISuite) TestResource(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	matchExp := fmt.Sprintf("Created resource %s and release %s.", UUIDRegex, UUIDRegex)
	t.Assert(app.flynn("resource", "add", "postgres").Output, Matches, matchExp)

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

func (s *CLISuite) TestResourceRemove(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()

	add := app.flynn("resource", "add", "postgres")
	t.Assert(add, Succeeds)
	t.Assert(app.flynn("resource").Output, Matches, "postgres")
	t.Assert(app.flynn("env").Output, Matches, "FLYNN_POSTGRES")
	id := strings.Split(add.Output, " ")[2]

	// change one of the env vars provided by the resource
	t.Assert(app.flynn("env", "set", "PGUSER=testuser"), Succeeds)

	remove := app.flynn("resource", "remove", "postgres", id)
	t.Assert(remove, Succeeds)

	t.Assert(app.flynn("resource").Output, c.Not(Matches), "postgres")
	// test that unmodified vars are removed
	t.Assert(app.flynn("env").Output, c.Not(Matches), "FLYNN_POSTGRES")
	// but that modifed ones are retained
	t.Assert(app.flynn("env", "get", "PGUSER").Output, Matches, "testuser")
}

func (s *CLISuite) TestLog(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	t.Assert(app.flynn("run", "-d", "echo", "hello", "world"), Succeeds)
	app.waitFor(ct.JobEvents{"": {ct.JobStateUp: 1, ct.JobStateDown: 1}})
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
	jobID := app.waitFor(ct.JobEvents{"": {ct.JobStateUp: 1, ct.JobStateDown: 1}})

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
	app.waitFor(ct.JobEvents{"": {ct.JobStateUp: 1, ct.JobStateDown: 1}})
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
	app.waitFor(ct.JobEvents{"": {ct.JobStateUp: 1}})

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
	t.Assert(flynn("cluster", "add", "--no-git", "foo", "https://controller.foo.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster"), SuccessfulOutputContains, "foo")
	t.Assert(flynn("cluster", "add", "--no-git", "-p", "KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs=", "test", "https://controller.test.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster"), SuccessfulOutputContains, "test")
	t.Assert(flynn("cluster", "add", "-f", "--no-git", "-p", "KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs=", "test", "https://controller.test.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster"), SuccessfulOutputContains, "test")
	t.Assert(flynn("cluster", "add", "-f", "-d", "--no-git", "-p", "KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs=", "test", "https://controller.test.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster"), SuccessfulOutputContains, "test")
	// make sure the cluster is present in the config
	cfg, err := config.ReadFile(file.Name())
	t.Assert(err, c.IsNil)
	t.Assert(cfg.Default, c.Equals, "test")
	t.Assert(cfg.Clusters, c.HasLen, 2)
	t.Assert(cfg.Clusters[0].Name, c.Equals, "foo")
	t.Assert(cfg.Clusters[1].Name, c.Equals, "test")
	// overwriting with a conflicting name and a different conflicting url should error
	conflict := flynn("cluster", "add", "-f", "--no-git", "foo", "https://controller.test.example.com", "e09dc5301d72be755a3d666f617c4600")
	t.Assert(conflict, c.Not(Succeeds))
	t.Assert(conflict, OutputContains, "conflict with")
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
	t.Assert(cfg.Clusters, c.HasLen, 1)
	t.Assert(flynn("cluster", "remove", "foo"), Succeeds)
	// cluster remove default and set next available
	t.Assert(flynn("cluster", "add", "-d", "--no-git", "-p", "KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs=", "test", "https://controller.test.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster", "add", "--no-git", "-p", "KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs=", "next", "https://controller.next.example.com", "e09dc5301d72be755a3d666f617c4600"), Succeeds)
	t.Assert(flynn("cluster", "remove", "test"), SuccessfulOutputContains, "Cluster \"test\" removed and \"next\" is now the default cluster.")
	t.Assert(flynn("cluster", "default"), SuccessfulOutputContains, "next")
}

func (s *CLISuite) TestRelease(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()

	release := &ct.Release{
		ArtifactIDs: []string{s.createArtifact(t, "test-apps").ID},
		Env:         map[string]string{"GLOBAL": "FOO"},
		Processes: map[string]ct.ProcessType{
			"echoer": {
				Args: []string{"/bin/echoer"},
				Env:  map[string]string{"ECHOER_ONLY": "BAR"},
			},
			"env": {
				Args: []string{"sh", "-c", "env; while true; do sleep 60; done"},
				Env:  map[string]string{"ENV_ONLY": "BAZ"},
			},
		},
	}
	client := s.controllerClient(t)
	t.Assert(client.CreateRelease(app.id, release), c.IsNil)
	t.Assert(client.SetAppRelease(app.id, release.ID), c.IsNil)

	updateFile := filepath.Join(t.MkDir(), "updates.json")
	updateJSON := []byte(`{
		"processes": {
			"echoer": {
				"env": {"ECHOER_ONLY": "BAT"}
			},
			"env": {
				"env": {"ENV_UPDATE": "QUUX"}
			}
		}
	}`)
	t.Assert(ioutil.WriteFile(updateFile, updateJSON, 0644), c.IsNil)
	t.Assert(app.flynn("release", "update", updateFile), Succeeds)

	resultJSON := []byte(`{
		"env": {"GLOBAL": "FOO"},
		"processes": {
			"echoer": {
				"args": ["/bin/echoer"],
				"env": {
					"ECHOER_ONLY": "BAT"
				}
			},
			"env": {
				"args": ["sh", "-c", "env; while true; do sleep 60; done"],
				"env": {
					"ENV_ONLY": "BAZ",
					"ENV_UPDATE": "QUUX"
				}
			}
		}
	}`)
	result := &ct.Release{}
	t.Assert(json.Unmarshal(resultJSON, &result), c.IsNil)
	for typ, proc := range result.Processes {
		resource.SetDefaults(&proc.Resources)
		result.Processes[typ] = proc
	}

	release, err := s.controller.GetAppRelease(app.name)
	t.Assert(err, c.IsNil)
	t.Assert(release.Env, c.DeepEquals, result.Env)
	t.Assert(release.Processes, c.DeepEquals, result.Processes)

	scaleCmd := app.flynn("scale", "--no-wait", "env=1", "foo=1")
	t.Assert(scaleCmd, c.Not(Succeeds))
	t.Assert(scaleCmd, OutputContains, "ERROR: unknown process types: \"foo\"")

	// create a job watcher for the new release
	watcher, err := client.WatchJobEvents(app.name, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	scaleCmd = app.flynn("scale", "--no-wait", "env=1")
	t.Assert(watcher.WaitFor(ct.JobEvents{"env": {ct.JobStateUp: 1}}, scaleTimeout, nil), c.IsNil)
	envLog := app.flynn("log")
	t.Assert(envLog, Succeeds)
	t.Assert(envLog, SuccessfulOutputContains, "GLOBAL=FOO")
	t.Assert(envLog, SuccessfulOutputContains, "ENV_ONLY=BAZ")
	t.Assert(envLog, SuccessfulOutputContains, "ENV_UPDATE=QUUX")
	t.Assert(envLog, c.Not(SuccessfulOutputContains), "ECHOER_ONLY=BAR")
	t.Assert(envLog, c.Not(SuccessfulOutputContains), "ECHOER_UPDATE=BAT")
	t.Assert(scaleCmd, Succeeds)
}

func (s *CLISuite) TestLimits(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	t.Assert(app.flynn("limit", "set", "resources", "memory=512MB", "max_fd=12k", "cpu=2000"), Succeeds)

	release, err := s.controller.GetAppRelease(app.name)
	t.Assert(err, c.IsNil)
	proc, ok := release.Processes["resources"]
	if !ok {
		t.Fatal("missing resources process type")
	}
	r := proc.Resources
	t.Assert(*r[resource.TypeMemory].Limit, c.Equals, int64(536870912))
	t.Assert(*r[resource.TypeCPU].Limit, c.Equals, int64(2000))
	t.Assert(*r[resource.TypeMaxFD].Limit, c.Equals, int64(12000))

	cmd := app.flynn("limit", "-t", "resources")
	t.Assert(cmd, Succeeds)
	t.Assert(cmd, OutputContains, "memory=512MB")
	t.Assert(cmd, OutputContains, "cpu=2000")
	t.Assert(cmd, OutputContains, "max_fd=12000")
}

func (s *CLISuite) TestRunLimits(t *c.C) {
	app := s.newCliTestApp(t)
	defer app.cleanup()
	cmd := app.flynn("run", "sh", "-c", resourceCmd)
	t.Assert(cmd, Succeeds)
	defaults := resource.Defaults()
	limits := strings.Split(strings.TrimSpace(cmd.Output), "\n")
	t.Assert(limits, c.HasLen, 3)
	t.Assert(limits[0], c.Equals, strconv.FormatInt(*defaults[resource.TypeMemory].Limit, 10))
	t.Assert(limits[1], c.Equals, strconv.FormatInt(1024, 10))
	t.Assert(limits[2], c.Equals, strconv.FormatInt(*defaults[resource.TypeMaxFD].Limit, 10))
	cmd = app.flynn("run", "--limits", "memory=200MB,max_fd=9000", "sh", "-c", resourceCmd)
	t.Assert(cmd, Succeeds)
	limits = strings.Split(strings.TrimSpace(cmd.Output), "\n")
	t.Assert(limits, c.HasLen, 3)
	t.Assert(limits[0], c.Equals, strconv.FormatInt(200*units.MiB, 10))
	t.Assert(limits[1], c.Equals, strconv.FormatInt(1024, 10))
	t.Assert(limits[2], c.Equals, strconv.FormatInt(9000, 10))
}

func assertExportContains(t *c.C, file string, paths ...string) {
	cmd := run(t, exec.Command("tar", "--list", "--file="+file, "--strip=1", "--show-transformed"))
	t.Assert(cmd, SuccessfulOutputContains, strings.Join(paths, "\n")+"\n")
}

func (s *CLISuite) TestExportImport(t *c.C) {
	srcApp := "app-export" + random.String(8)
	dstApp := "app-import" + random.String(8)

	// create app
	r := s.newGitRepo(t, "http")
	t.Assert(r.flynn("create", srcApp), Succeeds)

	// exporting the app without a release should work
	file := filepath.Join(t.MkDir(), "export.tar")
	t.Assert(r.flynn("export", "-f", file), Succeeds)
	assertExportContains(t, file, "app.json", "routes.json")

	// exporting the app with an artifact-less release should work
	t.Assert(r.flynn("env", "set", "FOO=BAR"), Succeeds)
	t.Assert(r.flynn("export", "-f", file), Succeeds)
	assertExportContains(t, file, "app.json", "routes.json", "release.json")

	// release the app and provision some dbs
	t.Assert(r.git("push", "flynn", "master"), Succeeds)
	t.Assert(r.flynn("resource", "add", "postgres"), Succeeds)
	t.Assert(r.flynn("pg", "psql", "--", "-c",
		"CREATE table foos (data text); INSERT INTO foos (data) VALUES ('foobar')"), Succeeds)
	t.Assert(r.flynn("resource", "add", "mysql"), Succeeds)
	t.Assert(r.flynn("mysql", "console", "--", "-e",
		"CREATE TABLE foos (data TEXT); INSERT INTO foos (data) VALUES ('foobar')"), Succeeds)

	// grab the slug details
	client := s.controllerClient(t)
	release, err := client.GetAppRelease(srcApp)
	t.Assert(err, c.IsNil)
	artifact, err := client.GetArtifact(release.ArtifactIDs[1])
	t.Assert(err, c.IsNil)
	slugLayer := artifact.Manifest().Rootfs[0].Layers[0]

	// export app
	t.Assert(r.flynn("export", "-f", file), Succeeds)
	assertExportContains(t, file,
		"app.json", "routes.json", "release.json", "artifacts.json",
		slugLayer.ID+".layer", "formation.json",
		"postgres.dump", "mysql.dump",
	)

	// remove db tables from source app
	t.Assert(r.flynn("pg", "psql", "--", "-c", "DROP TABLE foos"), Succeeds)
	t.Assert(r.flynn("mysql", "console", "--", "-e", "DROP TABLE foos"), Succeeds)

	// remove the git remote
	t.Assert(r.git("remote", "remove", "flynn"), Succeeds)

	// import app
	t.Assert(r.flynn("import", "--name", dstApp, "--file", file), Succeeds)

	// test dbs were imported
	query := r.flynn("-a", dstApp, "pg", "psql", "--", "-c", "SELECT * FROM foos")
	t.Assert(query, SuccessfulOutputContains, "foobar")
	query = r.flynn("-a", dstApp, "mysql", "console", "--", "-e", "SELECT * FROM foos")
	t.Assert(query, SuccessfulOutputContains, "foobar")

	// wait for it to start
	_, err = s.discoverdClient(t).Instances(dstApp+"-web", 10*time.Second)
	t.Assert(err, c.IsNil)
}

// TestExportBuildpackOutput tests that exporting a slug based app which
// outputs data before running commands (e.g. because the buildpack added a
// profile script which prints something) succeeds.
//
// See https://github.com/flynn/flynn/issues/3351
func (s *CLISuite) TestExportBuildpackOutput(t *c.C) {
	// create app
	r := s.newGitRepo(t, "http")
	t.Assert(r.flynn("create"), Succeeds)

	// create a profile script which prints some output when running the
	// slug
	t.Assert(r.sh("mkdir .profile.d && echo echo foo > .profile.d/echo.sh"), Succeeds)
	t.Assert(r.git("add", ".profile.d/echo.sh"), Succeeds)
	t.Assert(r.git("commit", "-m", "echo script"), Succeeds)

	// release the app
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	// check that exporting the app works
	t.Assert(r.flynn("export", "-f", "/dev/null"), Succeeds)
}

func (s *CLISuite) TestRemote(t *c.C) {
	remoteApp := "remote-" + random.String(8)
	customRemote := random.String(8)

	r := s.newGitRepoWithoutTrace(t, "http")
	// create app without remote
	t.Assert(r.flynn("create", remoteApp, "--remote", `""`), Succeeds)

	// ensure no remotes exist
	t.Assert(r.git("remote").Output, c.Equals, "\"\"\n")
	// create the default remote
	t.Assert(r.flynn("-a", remoteApp, "remote", "add"), Succeeds)
	// ensure the default remote exists
	t.Assert(r.git("remote", "show", "flynn"), Succeeds)
	// now delete it
	t.Assert(r.git("remote", "rm", "flynn"), Succeeds)

	// ensure no remotes exist
	t.Assert(r.git("remote").Output, c.Equals, "\"\"\n")
	// create a custom remote
	t.Assert(r.flynn("-a", remoteApp, "remote", "add", customRemote), Succeeds)
	// ensure the custom remote exists
	t.Assert(r.git("remote", "show", customRemote), Succeeds)
}

func (s *CLISuite) TestDeploy(t *c.C) {
	// create and push app
	r := s.newGitRepo(t, "http")
	t.Assert(r.flynn("create", "deploy-"+random.String(8)), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	deploy := r.flynn("deployment")
	t.Assert(deploy, Succeeds)
	t.Assert(deploy.Output, Matches, "complete")
}

func (s *CLISuite) TestDeployTimeout(t *c.C) {
	timeout := flynn(t, "/", "-a", "status", "deployment", "timeout")
	t.Assert(timeout, Succeeds)
	t.Assert(timeout.Output, c.Equals, "120\n")

	t.Assert(flynn(t, "/", "-a", "status", "deployment", "timeout", "150"), Succeeds)
	timeout = flynn(t, "/", "-a", "status", "deployment", "timeout")
	t.Assert(timeout, Succeeds)
	t.Assert(timeout.Output, c.Equals, "150\n")
}

func (s *CLISuite) TestReleaseDelete(t *c.C) {
	// create an app and release it twice
	r := s.newGitRepo(t, "http")
	app := "release-delete-" + random.String(8)
	t.Assert(r.flynn("create", app), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)
	t.Assert(r.git("commit", "--allow-empty", "--message", "empty commit"), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	// get the releases
	client := s.controllerClient(t)
	releases, err := client.AppReleaseList(app)
	t.Assert(err, c.IsNil)
	t.Assert(releases, c.HasLen, 2)

	// check the current release cannot be deleted
	res := r.flynn("release", "delete", "--yes", releases[0].ID)
	t.Assert(res, c.Not(Succeeds))
	t.Assert(res.Output, c.Equals, "validation_error: cannot delete current app release\n")

	// associate the initial release with another app
	otherApp := &ct.App{Name: "release-delete-" + random.String(8)}
	t.Assert(client.CreateApp(otherApp), c.IsNil)
	t.Assert(client.PutFormation(&ct.Formation{AppID: otherApp.ID, ReleaseID: releases[1].ID}), c.IsNil)

	// check deleting the initial release just deletes the formation
	res = r.flynn("release", "delete", "--yes", releases[1].ID)
	t.Assert(res, Succeeds)
	t.Assert(res.Output, c.Equals, "Release scaled down for app but not fully deleted (still associated with 1 other apps)\n")

	// check the slug artifact still exists
	slugArtifact, err := client.GetArtifact(releases[1].ArtifactIDs[1])
	t.Assert(err, c.IsNil)
	s.assertURI(t, slugArtifact.URI, http.StatusOK)
	slugLayerURL := slugArtifact.LayerURL(slugArtifact.Manifest().Rootfs[0].Layers[0])
	s.assertURI(t, slugLayerURL, http.StatusOK)

	// check the initial release can now be deleted
	res = r.flynn("-a", otherApp.ID, "release", "delete", "--yes", releases[1].ID)
	t.Assert(res, Succeeds)
	t.Assert(res.Output, c.Equals, fmt.Sprintf("Deleted release %s (deleted 2 files)\n", releases[1].ID))

	// check the slug artifact was deleted
	_, err = client.GetArtifact(slugArtifact.ID)
	t.Assert(err, c.Equals, controller.ErrNotFound)
	s.assertURI(t, slugArtifact.URI, http.StatusNotFound)
	s.assertURI(t, slugLayerURL, http.StatusNotFound)

	// check the image artifact was not deleted (since it is shared between both releases)
	_, err = client.GetArtifact(releases[1].ArtifactIDs[0])
	t.Assert(err, c.IsNil)
}

func (s *CLISuite) TestReleaseRollback(t *c.C) {
	// create an app and release it
	r := s.newGitRepo(t, "http")
	app := "release-rollback-" + random.String(8)
	t.Assert(r.flynn("create", app), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	// check that rollback fails when there's only a single release
	res := r.flynn("release", "rollback", "--yes")
	t.Assert(res, c.Not(Succeeds))

	// create a second release
	t.Assert(r.git("commit", "--allow-empty", "--message", "empty commit"), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	// get the releases
	client := s.controllerClient(t)
	releases, err := client.AppReleaseList(app)
	t.Assert(err, c.IsNil)
	t.Assert(releases, c.HasLen, 2)

	// rollback to the second release
	res = r.flynn("release", "rollback", "--yes")
	t.Assert(res, Succeeds)

	// revert rollback
	res = r.flynn("release", "rollback", "--yes", releases[0].ID)
	t.Assert(res, Succeeds)

	// check that attempting to rollback to the current release fails
	res = r.flynn("release", "rollback", "--yes", releases[0].ID)
	t.Assert(res, c.Not(Succeeds))
}

func (s *CLISuite) TestSlugReleaseGarbageCollection(t *c.C) {
	client := s.controllerClient(t)

	// create app with gc.max_inactive_slug_releases=3
	maxInactiveSlugReleases := 3
	app := &ct.App{Meta: map[string]string{"gc.max_inactive_slug_releases": strconv.Itoa(maxInactiveSlugReleases)}}
	t.Assert(client.CreateApp(app), c.IsNil)

	// create an image artifact
	imageArtifact := s.createArtifact(t, "test-apps")

	// create 5 slug artifacts
	tmp, err := ioutil.TempFile("", "squashfs-")
	t.Assert(err, c.IsNil)
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	t.Assert(exec.Command("mksquashfs", t.MkDir(), tmp.Name(), "-noappend").Run(), c.IsNil)
	slug, err := ioutil.ReadAll(tmp)
	t.Assert(err, c.IsNil)
	slugHash := sha512.Sum512(slug)
	slugs := []string{
		"http://blobstore.discoverd/layer/1.squashfs",
		"http://blobstore.discoverd/layer/2.squashfs",
		"http://blobstore.discoverd/layer/3.squashfs",
		"http://blobstore.discoverd/layer/4.squashfs",
		"http://blobstore.discoverd/layer/5.squashfs",
	}
	slugArtifacts := make([]*ct.Artifact, len(slugs))
	put := func(url string, data []byte) {
		req, err := http.NewRequest("PUT", url, bytes.NewReader(data))
		t.Assert(err, c.IsNil)
		res, err := http.DefaultClient.Do(req)
		t.Assert(err, c.IsNil)
		res.Body.Close()
		t.Assert(res.StatusCode, c.Equals, http.StatusOK)
	}
	for i, layerURL := range slugs {
		manifest := &ct.ImageManifest{
			Type: ct.ImageManifestTypeV1,
			Rootfs: []*ct.ImageRootfs{{
				Layers: []*ct.ImageLayer{{
					ID:     strconv.Itoa(i + 1),
					Type:   ct.ImageLayerTypeSquashfs,
					Length: int64(len(slug)),
					Hashes: map[string]string{"sha512": hex.EncodeToString(slugHash[:])},
				}},
			}},
		}
		data := manifest.RawManifest()
		url := fmt.Sprintf("http://blobstore.discoverd/image/%s.json", manifest.ID())
		put(url, data)
		put(layerURL, slug)
		artifact := &ct.Artifact{
			Type:             ct.ArtifactTypeFlynn,
			URI:              url,
			Meta:             map[string]string{"blobstore": "true"},
			RawManifest:      data,
			Hashes:           manifest.Hashes(),
			Size:             int64(len(data)),
			LayerURLTemplate: "http://blobstore.discoverd/layer/{id}.squashfs",
		}
		t.Assert(client.CreateArtifact(artifact), c.IsNil)
		slugArtifacts[i] = artifact
	}

	// create 6 releases, the second being scaled up and having the
	// same slug as the third (so prevents the slug being deleted)
	releases := make([]*ct.Release, 6)
	for i, r := range []struct {
		slug   *ct.Artifact
		active bool
	}{
		{slugArtifacts[0], false},
		{slugArtifacts[1], true},
		{slugArtifacts[1], false},
		{slugArtifacts[2], false},
		{slugArtifacts[3], false},
		{slugArtifacts[4], false},
	} {
		release := &ct.Release{
			ArtifactIDs: []string{imageArtifact.ID, r.slug.ID},
			Processes: map[string]ct.ProcessType{
				"app": {Args: []string{"/bin/pingserv"}, Ports: []ct.Port{{Proto: "tcp"}}},
			},
			Meta: map[string]string{"git": "true"},
		}
		t.Assert(client.CreateRelease(app.ID, release), c.IsNil)
		procs := map[string]int{"app": 0}
		if r.active {
			procs["app"] = 1
		}
		t.Assert(client.PutFormation(&ct.Formation{
			AppID:     app.ID,
			ReleaseID: release.ID,
			Processes: procs,
		}), c.IsNil)
		releases[i] = release
	}

	// scale the last release so we can deploy it
	lastRelease := releases[len(releases)-1]
	watcher, err := client.WatchJobEvents(app.ID, lastRelease.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()
	t.Assert(client.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: lastRelease.ID,
		Processes: map[string]int{"app": 1},
	}), c.IsNil)
	t.Assert(watcher.WaitFor(ct.JobEvents{"app": ct.JobUpEvents(1)}, scaleTimeout, nil), c.IsNil)
	t.Assert(client.SetAppRelease(app.ID, lastRelease.ID), c.IsNil)

	// subscribe to garbage collection events
	gcEvents := make(chan *ct.Event)
	stream, err := client.StreamEvents(ct.StreamEventsOptions{
		AppID:       app.ID,
		ObjectTypes: []ct.EventType{ct.EventTypeAppGarbageCollection},
	}, gcEvents)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	// deploy a new release with the same slug as the last release
	timeoutCh := make(chan struct{})
	time.AfterFunc(5*time.Minute, func() { close(timeoutCh) })
	newRelease := *lastRelease
	newRelease.ID = ""
	t.Assert(client.CreateRelease(app.ID, &newRelease), c.IsNil)
	t.Assert(client.DeployAppRelease(app.ID, newRelease.ID, timeoutCh), c.IsNil)

	// wait for garbage collection
	select {
	case event, ok := <-gcEvents:
		if !ok {
			t.Fatalf("event stream closed unexpectedly: %s", stream.Err())
		}
		var e ct.AppGarbageCollectionEvent
		t.Assert(json.Unmarshal(event.Data, &e), c.IsNil)
		if e.Error != "" {
			t.Fatalf("garbage collection failed: %s", e.Error)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("timed out waiting for garbage collection")
	}

	// check we have 4 distinct slug releases (so 5 in total, only 3 are
	// inactive)
	list, err := client.AppReleaseList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, maxInactiveSlugReleases+2)
	distinctSlugs := make(map[string]struct{}, len(list))
	for _, release := range list {
		t.Assert(release.ArtifactIDs, c.HasLen, 2)
		distinctSlugs[release.ArtifactIDs[1]] = struct{}{}
	}
	t.Assert(distinctSlugs, c.HasLen, maxInactiveSlugReleases+1)

	// check the first and third releases got deleted, but the rest remain
	assertDeleted := func(release *ct.Release, deleted bool) {
		_, err := client.GetRelease(release.ID)
		if deleted {
			t.Assert(err, c.Equals, controller.ErrNotFound)
		} else {
			t.Assert(err, c.IsNil)
		}
	}
	assertDeleted(releases[0], true)
	assertDeleted(releases[1], false)
	assertDeleted(releases[2], true)
	assertDeleted(releases[3], false)
	assertDeleted(releases[4], false)
	assertDeleted(releases[5], false)
	assertDeleted(&newRelease, false)

	// check the first slug got deleted, but the rest remain
	s.assertURI(t, slugs[0], http.StatusNotFound)
	for i := 1; i < len(slugs); i++ {
		s.assertURI(t, slugs[i], http.StatusOK)
	}
}

func (s *CLISuite) TestDockerPush(t *c.C) {
	// build HTTP image with ENV
	repo := "cli-test-push"
	s.buildHTTPDockerImage(t, repo, `ENV FOO=BAR`)

	// create app
	client := s.controllerClient(t)
	app := &ct.App{Name: "cli-test-docker-push"}
	t.Assert(client.CreateApp(app), c.IsNil)

	// flynn docker push image
	t.Assert(flynn(t, "/", "-a", app.Name, "docker", "push", repo), Succeeds)

	// check app was released with correct env, meta and process type
	release, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(release.Env["FOO"], c.Equals, "BAR")
	t.Assert(release.Meta["docker-receive"], c.Equals, "true")
	t.Assert(release.Processes, c.HasLen, 1)
	proc, ok := release.Processes["app"]
	if !ok {
		t.Fatal(`release missing "app" process type`)
	}
	t.Assert(proc.Args, c.DeepEquals, []string{"sh", "/server.sh"})

	// check updated env vars are not overwritten
	//
	// need to remove the tag before pushing as we are using Docker 1.9
	// which does not overwrite tags.
	// TODO: remove this when upgrading Docker > 1.9
	u, err := url.Parse(s.clusterConf(t).DockerPushURL)
	t.Assert(err, c.IsNil)
	tag := fmt.Sprintf("%s/%s:latest", u.Host, app.Name)
	t.Assert(run(t, exec.Command("docker", "rmi", tag)), Succeeds)
	t.Assert(flynn(t, "/", "-a", app.Name, "env", "set", "FOO=BAZ"), Succeeds)
	t.Assert(flynn(t, "/", "-a", app.Name, "docker", "push", repo), Succeeds)
	t.Assert(flynn(t, "/", "-a", app.Name, "env", "get", "FOO"), Outputs, "BAZ\n")

	// check the release can be scaled up
	t.Assert(flynn(t, "/", "-a", app.Name, "scale", "app=1"), Succeeds)

	// check the job is reachable with the app's name in discoverd
	instances, err := s.discoverdClient(t).Instances(app.Name+"-web", 10*time.Second)
	t.Assert(err, c.IsNil)
	res, err := hh.RetryClient.Get("http://" + instances[0].Addr)
	t.Assert(err, c.IsNil)
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	t.Assert(err, c.IsNil)
	t.Assert(string(body), c.Equals, "OK")
}

func (s *CLISuite) TestDockerExportImport(t *c.C) {
	// release via docker-receive
	client := s.controllerClient(t)
	app := &ct.App{Name: "cli-test-docker-export"}
	t.Assert(client.CreateApp(app), c.IsNil)
	repo := "cli-test-export"
	s.buildHTTPDockerImage(t, repo)
	t.Assert(flynn(t, "/", "-a", app.Name, "docker", "push", repo), Succeeds)
	t.Assert(flynn(t, "/", "-a", app.Name, "scale", "app=1"), Succeeds)
	defer flynn(t, "/", "-a", app.Name, "scale", "app=0")

	// grab the Flynn image layers
	release, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)
	artifact, err := client.GetArtifact(release.ArtifactIDs[0])
	t.Assert(err, c.IsNil)
	layers := artifact.Manifest().Rootfs[0].Layers
	layerNames := make([]string, len(layers))
	for i, layer := range layers {
		layerNames[i] = layer.ID + ".layer"
	}

	// check exporting to stdout works
	file := filepath.Join(t.MkDir(), "export.tar")
	cmd := exec.Command("sh", "-c", fmt.Sprintf("%s -a %s export > %s", args.CLI, app.Name, file))
	cmd.Env = flynnEnv(flynnrc)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if args.Stream {
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("error exporting docker app to stdout: %s: %s", err, stderr.String())
	}
	exportFiles := append([]string{
		"app.json", "routes.json", "release.json", "artifacts.json",
	}, append(layerNames, "formation.json")...)
	assertExportContains(t, file, exportFiles...)

	// export the app directly to the file
	t.Assert(flynn(t, "/", "-a", app.Name, "export", "-f", file), Succeeds)
	assertExportContains(t, file, exportFiles...)

	// delete the image from the registry
	u, err := url.Parse(s.clusterConf(t).DockerPushURL)
	t.Assert(err, c.IsNil)
	uri := fmt.Sprintf("http://%s/v2/%s/manifests/%s", u.Host, app.Name, artifact.Meta["docker-receive.digest"])
	req, err := http.NewRequest("DELETE", uri, nil)
	req.SetBasicAuth("", s.clusterConf(t).Key)
	t.Assert(err, c.IsNil)
	res, err := http.DefaultClient.Do(req)
	t.Assert(err, c.IsNil)
	res.Body.Close()

	// import to another app
	importApp := "cli-test-docker-import"
	t.Assert(flynn(t, "/", "import", "--name", importApp, "--file", file), Succeeds)
	defer flynn(t, "/", "-a", importApp, "scale", "app=0")

	// wait for it to start
	_, err = s.discoverdClient(t).Instances(importApp+"-web", 10*time.Second)
	t.Assert(err, c.IsNil)
}
