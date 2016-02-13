package main

import (
	"bytes"
	"fmt"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
)

type TaffyDeploySuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&TaffyDeploySuite{})

func (s *TaffyDeploySuite) deployWithTaffy(t *c.C, app *ct.App, env, meta, github map[string]string) {
	client := s.controllerClient(t)

	taffyRelease, err := client.GetAppRelease("taffy")
	t.Assert(err, c.IsNil)

	args := []string{
		app.Name,
		github["clone_url"],
		github["branch"],
		github["rev"],
	}

	for name, m := range map[string]map[string]string{"--env": env, "--meta": meta} {
		for k, v := range m {
			args = append(args, name)
			args = append(args, fmt.Sprintf("%s=%s", k, v))
		}
	}

	rwc, err := client.RunJobAttached("taffy", &ct.NewJob{
		ReleaseID:  taffyRelease.ID,
		ReleaseEnv: true,
		Cmd:        args,
		Meta: map[string]string{
			"github":      "true",
			"github_user": github["user"],
			"github_repo": github["repo"],
			"branch":      github["branch"],
			"rev":         github["rev"],
			"clone_url":   github["clone_url"],
			"app":         app.ID,
		},
	})
	t.Assert(err, c.IsNil)
	attachClient := cluster.NewAttachClient(rwc)
	var outBuf bytes.Buffer
	exit, err := attachClient.Receive(&outBuf, &outBuf)
	t.Log(outBuf.String())
	t.Assert(exit, c.Equals, 0)
	t.Assert(err, c.IsNil)
}

// This test emulates deploys in the dashboard app
func (s *TaffyDeploySuite) TestDeploys(t *c.C) {
	assertMeta := func(m map[string]string, k string, checker c.Checker, args ...interface{}) {
		v, ok := m[k]
		t.Assert(ok, c.Equals, true)
		t.Assert(v, checker, args...)
	}

	client := s.controllerClient(t)

	github := map[string]string{
		"user":      "flynn-examples",
		"repo":      "nodejs-flynn-example",
		"branch":    "master",
		"rev":       "5e177fec38fbde7d0a03e9e8dccf8757c68caa11",
		"clone_url": "https://github.com/flynn-examples/nodejs-flynn-example.git",
	}

	// initial deploy

	app := &ct.App{}
	t.Assert(client.CreateApp(app), c.IsNil)
	debugf(t, "created app %s (%s)", app.Name, app.ID)

	env := map[string]string{
		"SOMEVAR": "SOMEVAL",
	}
	meta := map[string]string{
		"github":      "true",
		"github_user": github["user"],
		"github_repo": github["repo"],
	}
	s.deployWithTaffy(t, app, env, meta, github)

	release, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(release, c.NotNil)
	t.Assert(release.Meta, c.NotNil)
	assertMeta(release.Meta, "git", c.Equals, "true")
	assertMeta(release.Meta, "clone_url", c.Equals, github["clone_url"])
	assertMeta(release.Meta, "branch", c.Equals, github["branch"])
	assertMeta(release.Meta, "rev", c.Equals, github["rev"])
	assertMeta(release.Meta, "taffy_job", c.Not(c.Equals), "")
	assertMeta(release.Meta, "github", c.Equals, "true")
	assertMeta(release.Meta, "github_user", c.Equals, github["user"])
	assertMeta(release.Meta, "github_repo", c.Equals, github["repo"])
	t.Assert(release.Env, c.NotNil)
	assertMeta(release.Env, "SOMEVAR", c.Equals, "SOMEVAL")

	// second deploy

	github["rev"] = "4231f8871da2b9fd73a5402753df3dfc5609d7b7"

	release, err = client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)

	s.deployWithTaffy(t, app, env, meta, github)

	newRelease, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(newRelease.ID, c.Not(c.Equals), release.ID)
	t.Assert(env, c.DeepEquals, newRelease.Env)
	t.Assert(release.Processes, c.DeepEquals, newRelease.Processes)
	t.Assert(newRelease, c.NotNil)
	t.Assert(newRelease.Meta, c.NotNil)
	assertMeta(newRelease.Meta, "git", c.Equals, "true")
	assertMeta(newRelease.Meta, "clone_url", c.Equals, github["clone_url"])
	assertMeta(newRelease.Meta, "branch", c.Equals, github["branch"])
	assertMeta(newRelease.Meta, "rev", c.Equals, github["rev"])
	assertMeta(newRelease.Meta, "taffy_job", c.Not(c.Equals), "")
	assertMeta(newRelease.Meta, "github", c.Equals, "true")
	assertMeta(newRelease.Meta, "github_user", c.Equals, github["user"])
	assertMeta(newRelease.Meta, "github_repo", c.Equals, github["repo"])
}
