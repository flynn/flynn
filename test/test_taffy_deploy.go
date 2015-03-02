package main

import (
	"bytes"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
)

type TaffyDeploySuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&TaffyDeploySuite{})

func (s *TaffyDeploySuite) deployWithTaffy(t *c.C, app *ct.App, github map[string]string) {
	client := s.controllerClient(t)

	taffyRelease, err := client.GetAppRelease("taffy")
	t.Assert(err, c.IsNil)

	rwc, err := client.RunJobAttached("taffy", &ct.NewJob{
		ReleaseID:  taffyRelease.ID,
		ReleaseEnv: true,
		Cmd: []string{
			app.Name,
			github["clone_url"],
			github["ref"],
			github["sha"],
		},
		Meta: map[string]string{
			"type":       "github",
			"user_login": github["user_login"],
			"repo_name":  github["repo_name"],
			"ref":        github["ref"],
			"sha":        github["sha"],
			"clone_url":  github["clone_url"],
			"app":        app.ID,
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
	client := s.controllerClient(t)

	github := map[string]string{
		"user_login": "flynn-examples",
		"repo_name":  "go-flynn-example",
		"ref":        "master",
		"sha":        "a2ac6b059e1359d0e974636935fda8995de02b16",
		"clone_url":  "https://github.com/flynn-examples/go-flynn-example.git",
	}

	// initial deploy

	app := &ct.App{
		Meta: map[string]string{
			"type":       "github",
			"user_login": github["user_login"],
			"repo_name":  github["repo_name"],
			"ref":        github["ref"],
			"sha":        github["sha"],
			"clone_url":  github["clone_url"],
		},
	}
	t.Assert(client.CreateApp(app), c.IsNil)
	debugf(t, "created app %s (%s)", app.Name, app.ID)

	s.deployWithTaffy(t, app, github)

	_, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)

	// second deploy

	github["sha"] = "2bc7e016b1b4aae89396c898583763c5781e031a"

	release, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)

	release = &ct.Release{
		Env:       release.Env,
		Processes: release.Processes,
	}
	t.Assert(client.CreateRelease(release), c.IsNil)
	t.Assert(client.SetAppRelease(app.ID, release.ID), c.IsNil)

	s.deployWithTaffy(t, app, github)

	newRelease, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(newRelease.ID, c.Not(c.Equals), release.ID)
	release.Env["SLUG_URL"] = newRelease.Env["SLUG_URL"] // SLUG_URL will be different
	t.Assert(release.Env, c.DeepEquals, newRelease.Env)
	t.Assert(release.Processes, c.DeepEquals, newRelease.Processes)
}
