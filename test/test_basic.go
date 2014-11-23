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
	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/random"
)

type GitDeploySuite struct {
	ssh    *sshData
	client *controller.Client
}

var _ = c.Suite(&GitDeploySuite{})

func (s *GitDeploySuite) SetUpSuite(t *c.C) {
	var err error
	s.ssh, err = genSSHKey()
	t.Assert(err, c.IsNil)
	t.Assert(flynn("/", "key", "add", s.ssh.Pub), Succeeds)

	conf, err := config.ReadFile(flynnrc)
	t.Assert(err, c.IsNil)
	t.Assert(conf.Clusters, c.HasLen, 1)
	s.client = newControllerClient(t, conf.Clusters[0])
}

func (s *GitDeploySuite) TearDownSuite(t *c.C) {
	if s.ssh != nil {
		s.ssh.Cleanup()
	}
	if s.client != nil {
		s.client.Close()
	}
}

type gitRepo struct {
	dir string
	ssh *sshData
}

func (s *GitDeploySuite) newGitRepo(t *c.C, nameOrURL string) *gitRepo {
	dir := filepath.Join(t.MkDir(), "repo")
	r := &gitRepo{dir, s.ssh}

	if strings.HasPrefix(nameOrURL, "https://") {
		t.Assert(run(exec.Command("git", "clone", nameOrURL, dir)), Succeeds)
		return r
	}

	t.Assert(run(exec.Command("cp", "-r", filepath.Join("apps", nameOrURL), dir)), Succeeds)
	t.Assert(r.git("init"), Succeeds)
	t.Assert(r.git("add", "."), Succeeds)
	t.Assert(r.git("commit", "-am", "init"), Succeeds)
	return r
}

func (r *gitRepo) flynn(args ...string) *CmdResult {
	return flynn(r.dir, args...)
}

func (r *gitRepo) git(args ...string) *CmdResult {
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), r.ssh.Env...)
	cmd.Dir = r.dir
	return run(cmd)
}

var Attempts = attempt.Strategy{
	Total: 20 * time.Second,
	Delay: 500 * time.Millisecond,
}

func (s *GitDeploySuite) TestBasic(t *c.C) {
	r := s.newGitRepo(t, "basic")

	name := random.String(30)
	t.Assert(r.flynn("create", name), Outputs, fmt.Sprintf("Created %s\n", name))

	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)

	t.Assert(push, OutputContains, "Node.js app detected")
	t.Assert(push, OutputContains, "Downloading and installing node")
	t.Assert(push, OutputContains, "Installing dependencies")
	t.Assert(push, OutputContains, "Procfile declares types -> web")
	t.Assert(push, OutputContains, "Creating release")
	t.Assert(push, OutputContains, "Application deployed")
	t.Assert(push, OutputContains, "* [new branch]      master -> master")

	defer r.flynn("scale", "web=0")
	t.Assert(r.flynn("scale", "web=3"), Succeeds)

	route := random.String(32) + ".dev"
	newRoute := r.flynn("route", "add", "http", route)
	t.Assert(newRoute, Succeeds)

	t.Assert(r.flynn("route"), OutputContains, strings.TrimSpace(newRoute.Output))

	// use Attempts to give the processes time to start
	if err := Attempts.Run(func() error {
		ps := r.flynn("ps")
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
			log := r.flynn("log", idType[0])
			if !strings.Contains(log.Output, "Listening on ") {
				return fmt.Errorf("Expected \"%s\" to contain \"Listening on \"", log.Output)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Make HTTP requests
	client := &http.Client{}
	req, err := http.NewRequest("GET", "http://"+routerIP, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = route
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	contents, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	t.Assert(res.StatusCode, c.Equals, 200)
	t.Assert(string(contents), Matches, `Hello to Yahoo from Flynn on port \d+`)
}

func (s *GitDeploySuite) TestEnvDir(t *c.C) {
	r := s.newGitRepo(t, "env-dir")
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.flynn("env", "set", "FOO=bar", "BUILDPACK_URL=https://github.com/kr/heroku-buildpack-inline"), Succeeds)

	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, OutputContains, "bar")
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

func (s *GitDeploySuite) TestGrailsBuildpack(t *c.C) {
	s.runBuildpackTest(t, "grails-flynn-example", nil)
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

	stream, err := s.client.StreamJobEvents(name, 0)
	t.Assert(err, c.IsNil)

	r.flynn("scale", "web=0")
	// wait for the jobs to stop
	waitForJobEvents(t, stream.Events, map[string]int{"web": -1})
	stream.Close()
}
