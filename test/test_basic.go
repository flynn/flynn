package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/check.v1"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/random"
)

type appSuite struct {
	appDir string
	ssh    *sshData
}

func (s *appSuite) initApp(t *c.C, app string) {
	s.appDir = filepath.Join(t.MkDir(), "app")
	t.Assert(run(exec.Command("cp", "-r", filepath.Join("apps", app), s.appDir)), Succeeds)
	t.Assert(s.Git("init"), Succeeds)
	t.Assert(s.Git("add", "."), Succeeds)
	t.Assert(s.Git("commit", "-am", "init"), Succeeds)
}

func (s *appSuite) Flynn(args ...string) *CmdResult {
	return flynn(s.appDir, args...)
}

func (s *appSuite) Git(args ...string) *CmdResult {
	cmd := exec.Command("git", args...)
	if s.ssh != nil {
		cmd.Env = append(os.Environ(), s.ssh.Env...)
	}
	cmd.Dir = s.appDir
	return run(cmd)
}

type BasicSuite struct {
	appSuite
}

var _ = c.Suite(&BasicSuite{})

func (s *BasicSuite) SetUpSuite(t *c.C) {
	s.initApp(t, "basic")
}

var Attempts = attempt.Strategy{
	Total: 20 * time.Second,
	Delay: 500 * time.Millisecond,
}

func (s *BasicSuite) TestBasic(t *c.C) {
	var err error
	s.ssh, err = genSSHKey()
	t.Assert(err, c.IsNil)
	defer s.ssh.Cleanup()

	t.Assert(s.Flynn("key", "add", s.ssh.Pub).Err, c.IsNil)

	name := random.String(30)
	t.Assert(s.Flynn("create", name), Outputs, fmt.Sprintf("Created %s\n", name))

	push := s.Git("push", "flynn", "master")
	t.Assert(push, OutputContains, "Node.js app detected")
	t.Assert(push, OutputContains, "Downloading and installing node")
	t.Assert(push, OutputContains, "Installing dependencies")
	t.Assert(push, OutputContains, "Procfile declares types -> web")
	t.Assert(push, OutputContains, "Creating release")
	t.Assert(push, OutputContains, "Application deployed")
	t.Assert(push, OutputContains, "* [new branch]      master -> master")

	t.Assert(s.Flynn("scale", "web=3"), Succeeds)

	route := random.String(32) + ".dev"
	newRoute := s.Flynn("route", "add", "-t", "http", route)
	t.Assert(newRoute, Succeeds)

	t.Assert(s.Flynn("route"), OutputContains, strings.TrimSpace(newRoute.Output))

	// use Attempts to give the processes time to start
	if err := Attempts.Run(func() error {
		ps := s.Flynn("ps")
		if ps.Err != nil {
			return ps.Err
		}
		psLines := strings.Split(strings.TrimSpace(ps.Output), "\n")
		if len(psLines) != 4 {
			return fmt.Errorf("Expected 4 ps lines, got %d", len(psLines))
		}

		for _, l := range psLines[1:] {
			idType := regexp.MustCompile(`\s+`).Split(l, 2)
			if idType[1] != "web" {
				return fmt.Errorf("Expected web type, got %s", idType[1])
			}
			log := s.Flynn("log", idType[0])
			if !strings.Contains(log.Output, "Listening on ") {
				return fmt.Errorf("Expected \"%s\" to contain \"Listening on \"", log.Output)
			}
		}
		return nil
	}); err != nil {
		t.Error(err)
	}

	// Make HTTP requests
	client := &http.Client{}
	req, err := http.NewRequest("GET", "http://"+routerIP, nil)
	if err != nil {
		t.Error(err)
	}
	req.Host = route
	res, err := client.Do(req)
	if err != nil {
		t.Error(err)
	}
	defer res.Body.Close()
	contents, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	t.Assert(res.StatusCode, c.Equals, 200)
	t.Assert(string(contents), Matches, `Hello to Yahoo from Flynn on port \d+`)
}
