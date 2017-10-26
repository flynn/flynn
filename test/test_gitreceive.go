package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	c "github.com/flynn/go-check"
)

type GitreceiveSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&GitreceiveSuite{})

func (s *GitreceiveSuite) TestRepoCaching(t *c.C) {
	x := s.bootCluster(t, 1)
	defer x.Destroy()

	r := s.newGitRepo(t, "empty")
	r.cluster = x
	t.Assert(r.flynn("create"), Succeeds)

	r.git("commit", "-m", "bump", "--allow-empty")
	r.git("commit", "-m", "bump", "--allow-empty")
	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, c.Not(OutputContains), "cached")

	// cycle the receiver to clear any cache
	t.Assert(x.flynn("/", "-a", "gitreceive", "scale", "app=0"), Succeeds)
	t.Assert(x.flynn("/", "-a", "gitreceive", "scale", "app=1"), Succeeds)
	_, err := x.discoverd.Instances("gitreceive", 10*time.Second)
	t.Assert(err, c.IsNil)

	r.git("commit", "-m", "bump", "--allow-empty")
	push = r.git("push", "flynn", "master", "--progress")
	// should only contain one object
	t.Assert(push, SuccessfulOutputContains, "Counting objects: 1, done.")
}

func (s *GitreceiveSuite) TestSlugbuilderLimit(t *c.C) {
	x := s.bootCluster(t, 1)
	defer x.Destroy()

	r := s.newGitRepo(t, "slugbuilder-limit")
	r.cluster = x
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.flynn("env", "set", "BUILDPACK_URL=https://github.com/kr/heroku-buildpack-inline"), Succeeds)
	t.Assert(r.flynn("-a", "gitreceive", "env", "set", "SLUGBUILDER_DEFAULT_MEMORY_LIMIT=2GB"), Succeeds)

	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, OutputContains, "2147483648")

	t.Assert(r.flynn("limit", "set", "slugbuilder", "memory=500MB"), Succeeds)

	t.Assert(r.git("commit", "-m", "bump", "--allow-empty"), Succeeds)
	push = r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, OutputContains, "524288000")

	limit := r.flynn("limit")
	t.Assert(limit, Succeeds)
	t.Assert(limit.Output, Matches, "slugbuilder:.+memory=500MB")

	t.Assert(r.flynn("-a", "gitreceive", "env", "unset", "SLUGBUILDER_DEFAULT_MEMORY_LIMIT"), Succeeds)
}

func (s *GitreceiveSuite) TestDeployWithEnv(t *c.C) {
	appDir := filepath.Join("apps", "env-dir")
	client := s.controllerClient(t)
	app := &ct.App{}
	t.Assert(client.CreateApp(app), c.IsNil)
	debugf(t, "created app %s (%s)", app.Name, app.ID)

	tarResult := run(t, exec.Command("sh", "-c", fmt.Sprintf("tar --create --directory %s .", appDir)))

	env := map[string]string{
		"FOO":           "BAR",
		"BUILDPACK_URL": "git@github.com:kr/heroku-buildpack-inline.git",
	}
	args := []string{"-a", "gitreceive", "run", "/bin/flynn-receiver", app.Name, "test-rev"}
	for k, v := range env {
		args = append(args, "--env")
		args = append(args, fmt.Sprintf("%s=%s", k, v))
	}
	cmd := flynnCmd(appDir, args...)
	cmd.Stdin = tarResult.OutputBuf
	result := run(t, cmd)

	t.Assert(result, Succeeds)
	t.Assert(result, OutputContains, "BAR")
	t.Assert(result.Err, c.IsNil)

	t.Assert(tarResult, Succeeds)
	t.Assert(tarResult.Err, c.IsNil)
}

func (s *GitreceiveSuite) TestGitReleaseMeta(t *c.C) {
	x := s.bootCluster(t, 1)
	defer x.Destroy()

	r := s.newGitRepo(t, "empty")
	r.cluster = x
	r.trace = false
	app := "test-git-release-meta"
	t.Assert(r.flynn("create", app), Succeeds)

	t.Assert(r.git("commit", "-m", "bump", "--allow-empty"), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	release, err := x.controller.GetAppRelease(app)
	t.Assert(err, c.IsNil)
	commit := strings.TrimSpace(r.git("rev-parse", "HEAD").Output)
	t.Assert(commit, c.Not(c.Equals), "")
	t.Assert(release.Meta, c.DeepEquals, map[string]string{
		"git":        "true",
		"git.commit": commit,
	})
}
