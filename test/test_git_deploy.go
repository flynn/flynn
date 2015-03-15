package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/pkg/attempt"
)

type GitDeploySuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&GitDeploySuite{})

func (s *GitDeploySuite) SetUpSuite(t *c.C) {
	t.Assert(flynn(t, "/", "key", "add", s.sshKeys(t).Pub), Succeeds)
}

var Attempts = attempt.Strategy{
	Total: 60 * time.Second,
	Delay: 500 * time.Millisecond,
}

func (s *GitDeploySuite) TestEnvDir(t *c.C) {
	r := s.newGitRepo(t, "env-dir")
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.flynn("env", "set", "FOO=bar", "BUILDPACK_URL=https://github.com/kr/heroku-buildpack-inline"), Succeeds)

	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, OutputContains, "bar")
}

func (s *GitDeploySuite) TestBuildCaching(t *c.C) {
	r := s.newGitRepo(t, "build-cache")
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.flynn("env", "set", "BUILDPACK_URL=https://github.com/kr/heroku-buildpack-inline"), Succeeds)

	r.git("commit", "-m", "bump", "--allow-empty")
	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, c.Not(OutputContains), "cached")

	r.git("commit", "-m", "bump", "--allow-empty")
	push = r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, OutputContains, "cached: 0")

	r.git("commit", "-m", "bump", "--allow-empty")
	push = r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, OutputContains, "cached: 1")
}

func (s *GitDeploySuite) TestAppRecreation(t *c.C) {
	r := s.newGitRepo(t, "empty")
	t.Assert(r.flynn("create", "-y", "app-recreation"), Succeeds)
	r.git("commit", "-m", "bump", "--allow-empty")
	t.Assert(r.git("push", "flynn", "master"), Succeeds)
	t.Assert(r.flynn("delete", "-y"), Succeeds)

	// recreate app and push again, it should work
	t.Assert(r.flynn("create", "-y", "app-recreation"), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)
	t.Assert(r.flynn("delete", "-y"), Succeeds)
}

func (s *GitDeploySuite) TestGoBuildpack(t *c.C) {
	s.runBuildpackTest(t, "go-flynn-example", []string{"postgres"})
}

func (s *GitDeploySuite) TestNodejsBuildpack(t *c.C) {
	s.runBuildpackTest(t, "nodejs-flynn-example", nil)
}

func (s *GitDeploySuite) TestPhpBuildpack(t *c.C) {
	s.runBuildpackTest(t, "php-flynn-example", nil)
}

func (s *GitDeploySuite) TestRubyBuildpack(t *c.C) {
	s.runBuildpackTest(t, "ruby-flynn-example", nil)
}

func (s *GitDeploySuite) TestJavaBuildpack(t *c.C) {
	s.runBuildpackTest(t, "java-flynn-example", nil)
}

func (s *GitDeploySuite) TestClojureBuildpack(t *c.C) {
	s.runBuildpackTest(t, "clojure-flynn-example", nil)
}

func (s *GitDeploySuite) TestGradleBuildpack(t *c.C) {
	s.runBuildpackTest(t, "gradle-flynn-example", nil)
}

func (s *GitDeploySuite) TestPlayBuildpack(t *c.C) {
	s.runBuildpackTest(t, "play-flynn-example", nil)
}

func (s *GitDeploySuite) TestPythonBuildpack(t *c.C) {
	s.runBuildpackTest(t, "python-flynn-example", nil)
}

func (s *GitDeploySuite) runBuildpackTest(t *c.C, name string, resources []string) {
	r := s.newGitRepo(t, "https://github.com/flynn-examples/"+name)

	t.Assert(r.flynn("create", name), Outputs, fmt.Sprintf("Created %s\n", name))

	for _, resource := range resources {
		t.Assert(r.flynn("resource", "add", resource), Succeeds)
	}

	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, OutputContains, "Creating release")
	t.Assert(push, OutputContains, "Application deployed")
	t.Assert(push, OutputContains, "* [new branch]      master -> master")

	t.Assert(r.flynn("scale", "web=1"), Succeeds)

	route := name + ".dev"
	newRoute := r.flynn("route", "add", "http", route)
	t.Assert(newRoute, Succeeds)

	err := Attempts.Run(func() error {
		// Make HTTP requests
		client := &http.Client{}
		req, err := http.NewRequest("GET", "http://"+routerIP, nil)
		if err != nil {
			return err
		}
		req.Host = route
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		contents, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return err
		}
		if res.StatusCode != 200 {
			return fmt.Errorf("Expected status 200, got %v", res.StatusCode)
		}
		m, err := regexp.MatchString(`Hello from Flynn on port \d+`, string(contents))
		if err != nil {
			return err
		}
		if !m {
			return fmt.Errorf("Expected `Hello from Flynn on port \\d+`, got `%v`", string(contents))
		}
		return nil
	})
	t.Assert(err, c.IsNil)

	t.Assert(r.flynn("scale", "web=0"), Succeeds)
}
