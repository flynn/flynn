package main

import (
	"bytes"
	"fmt"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
	c "github.com/flynn/go-check"
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
		"/bin/taffy",
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
		Args:       args,
		Meta: map[string]string{
			"github":      "true",
			"github_user": github["user"],
			"github_repo": github["repo"],
			"branch":      github["branch"],
			"rev":         github["rev"],
			"clone_url":   github["clone_url"],
			"app":         app.ID,
		},
		Env: env,
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

// Test taffy's ability to deploy repos that require key authentication
func (s *TaffyDeploySuite) TestPrivateDeploys(t *c.C) {
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
		"clone_url": "git@github.com:/flynn-examples/nodejs-flynn-example.git",
	}

	app := &ct.App{}
	t.Assert(client.CreateApp(app), c.IsNil)
	debugf(t, "created app %s (%s)", app.Name, app.ID)

	sshKey := `MIIEpAIBAAKCAQEA2UnQ/17TfzQRt4HInuP1SYz/tSNaCGO3NDIPLydVu8mmxuKT
zlJtH3pz3uWpMEKdZtSjV+QngJL8OFzanQVZtRBJjF2m+cywHJoZA5KsplMon+R+
QmVqu92WlcRdkcft1F1CLoTXTmHHfvuhOkG6GgJONNLP9Z14EsQ7MbBh5guafWOX
kdGFajyd+T2aj27yIkK44WjWqiLjxRIAtgOJrmd/3H0w3E+O1cgNrA2gkFEUhvR1
OHz8SmugYva0VZWKvxZ6muZvn26L1tajYsCntCRR3/a74cAnVFAXjqSatL6YTbSH
sdtE91kEC73/U4SL3OFdDiCrAvXpJ480C2/GQQIDAQABAoIBAHNQNVYRIPS00WIt
wiZwm8/4wAuFQ1aIdMWCe4Ruv5T1I0kRHZe1Lqwx9CQqhWtTLu1Pk5AlSMF3P9s5
i9sg58arahzP5rlS43OKZBP9Vxq9ryWLwWXDJK2mny/EElQ3YgP9qg29+fVi9thw
+dNM5lK/PnnSFwMmGn77HN712D6Yl3CCJJjsAunTfPzR9hyEqX5YvUB5eq/TNhXe
sqrKcGORIoNfv7WohlFSkTAXIvoMxmFWXg8piZ9/b1W4NwvO4wup3ZSErIk0AQ97
HtyXJIXgtj6pLkPqvPXPGvS3quYAddNxvGIdvge7w5LHnrxOzdqbeDAVmJLVwVlv
oo+7aQECgYEA8ZliUuA8q86SWE0N+JZUqbTvE6VzyWG0/u0BJYDkH7yHkbpFOIEy
KTw048WOZLQ6/wPwL8Hb090Cas/6pmRFMgCedarzXc9fvGEwW95em7jA4AyOVBMC
KIAmaYkm6LcUFeyR6ektZeCkT0MNoi4irjBC3/hMRyZu+6RL4jXxHLkCgYEA5j13
2nkbV99GtRRjyGB7uMkrhMere2MekANXEm4dW+LZFZUda4YCqdzfjDfBTxsuyGqi
DnvI7bZFzIQPiiEzvL2Mpiy7JqxmPLGmwzxDp3z75T5vOrGs4g9IQ7yDjp5WPzjz
KCJJHn8Qt9tNZb5h0hBM+NWLT0c1XxtTIVFfgckCgYAfNpTYZjYQcFDB7bqXWjy3
7DNTE3YhF2l94fra8IsIep/9ONaGlVJ4t1mR780Uv6A7oDOgx+fxuET+rb4RTzUN
X70ZMKvee9M/kELiK5mHftgUWirtO8N0nhHYYqrPOA/1QSoc0U5XMi2oO96ADHvY
i02oh/i63IFMK47OO+/ZqQKBgQCY8bY/Y/nc+o4O1hee0TD+xGvrTXRFh8eSpRVf
QdSw6FWKt76OYbw9OGMr0xHPyd/e9K7obiRAfLeLLyLfgETNGSFodghwnU9g/CYq
RUsv5J+0XjAnTkXo+Xvouz6tK9NhNiSYwYXPA1uItt6IOtriXz+ygLCFHml+3zju
xg5quQKBgQCEL95Di6WD+155gEG2NtqeAOWhgxqAbGjFjfpV+pVBksBCrWOHcBJp
QAvAdwDIZpqRWWMcLS7zSDrzn3ZscuHCMxSOe40HbrVdDUee24/I4YQ+R8EcuzcA
3IV9ai+Bxs6PvklhXmarYxJl62LzPLyv0XFscGRes/2yIIxNfNzFug==`

	env := map[string]string{
		"SOMEVAR":          "SOMEVAL",
		"SSH_CLIENT_HOSTS": "github.com,192.30.252.131 ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAQEAq2A7hRGmdnm9tUDbO9IDSwBK6TbQa+PXYPCPy6rbTrTtw7PHkccKrpp0yVhp5HdEIcKr6pLlVDBfOLX9QUsyCOV0wzfjIJNlGEYsdlLJizHhbn2mUjvSAHQqZETYP81eFzLQNnPHt4EVVUh7VfDESU84KezmD5QlWpXLmvU31/yMf+Se8xhHTvKSCZIFImWwoG6mbUoWf9nzpIoaSjB+weqqUUmpaaasXVal72J+UX2B+2RPW3RcT0eOzQgqlJL3RKrTJvdsjE3JEAvGq3lGHSZXy28G3skua2SmVi/w4yCE6gbODqnTWlg7+wC604ydGXA8VJiS5ap43JXiUFFAaQ==",
		"SSH_CLIENT_KEY":   fmt.Sprintf("-----BEGIN RSA PRIVATE KEY-----\n%s\n-----END RSA PRIVATE KEY-----\n", sshKey),
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
}
