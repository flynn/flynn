package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/check.v1"
	ct "github.com/flynn/flynn/controller/types"
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

	var push *CmdResult
	if err := Attempts.Run(func() error {
		ps := s.Flynn("-a", "gitreceive", "ps")
		if ps.Err != nil {
			return ps.Err
		}
		psLines := strings.Split(strings.TrimSpace(ps.Output), "\n")
		if len(psLines) != 2 {
			return fmt.Errorf("Expected 2 ps lines, got %d", len(psLines))
		}
		push = s.Git("push", "flynn", "master")
		return push.Err
	}); err != nil {
		t.Error(err)
	}

	t.Assert(push, OutputContains, "Node.js app detected")
	t.Assert(push, OutputContains, "Downloading and installing node")
	t.Assert(push, OutputContains, "Installing dependencies")
	t.Assert(push, OutputContains, "Procfile declares types -> web")
	t.Assert(push, OutputContains, "Creating release")
	t.Assert(push, OutputContains, "Application deployed")
	t.Assert(push, OutputContains, "* [new branch]      master -> master")

	t.Assert(s.Flynn("scale", "web=3"), Succeeds)

	route := random.String(32) + ".dev"
	newRoute := s.Flynn("route", "add", "http", route)
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

func (s *SchedulerSuite) TestTCPApp(t *c.C) {
	r, err := s.client.GetAppRelease("gitreceive")
	t.Assert(err, c.IsNil)
	imageURI := r.Processes["app"].Env["SLUGRUNNER_IMAGE_URI"]

	app := &ct.App{}
	t.Assert(s.client.CreateApp(app), c.IsNil)

	artifact := &ct.Artifact{Type: "docker", URI: imageURI}
	t.Assert(s.client.CreateArtifact(artifact), c.IsNil)

	release := &ct.Release{
		ArtifactID: artifact.ID,
		Processes: map[string]ct.ProcessType{
			"echo": {
				Ports:      []ct.Port{{Proto: "tcp"}},
				Cmd:        []string{"sdutil exec -s echo-service:$PORT socat -v tcp-l:$PORT,fork exec:/bin/cat"},
				Entrypoint: []string{"sh", "-c"},
			},
		},
	}
	t.Assert(s.client.CreateRelease(release), c.IsNil)
	t.Assert(s.client.SetAppRelease(app.ID, release.ID), c.IsNil)

	stream, err := s.client.StreamJobEvents(app.ID)
	defer stream.Close()
	if err != nil {
		t.Error(err)
	}

	t.Assert(flynn("/", "-a", app.Name, "scale", "echo=1"), Succeeds)

	newRoute := flynn("/", "-a", app.Name, "route", "add", "tcp", "-s", "echo-service")
	t.Assert(newRoute, Succeeds)
	t.Assert(newRoute.Output, Matches, `.+ on port \d+`)
	str := strings.Split(strings.TrimSpace(string(newRoute.Output)), " ")
	port := str[len(str)-1]

	waitForJobEvents(t, stream.Events, map[string]int{"echo": 1})
	// use Attempts to give the processes time to start
	if err := Attempts.Run(func() error {
		servAddr := routerIP + ":" + port
		conn, err := net.Dial("tcp", servAddr)
		defer conn.Close()
		if err != nil {
			return err
		}
		echo := random.Bytes(16)
		_, err = conn.Write(echo)
		if err != nil {
			return err
		}
		reply := make([]byte, 16)
		_, err = conn.Read(reply)
		if err != nil {
			return err
		}
		t.Assert(reply, c.DeepEquals, echo)
		return nil
	}); err != nil {
		t.Error(err)
	}
}
