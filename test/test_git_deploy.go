package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/attempt"
)

type GitDeploySuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&GitDeploySuite{})

func (s *GitDeploySuite) SetUpSuite(t *c.C) {
	// Unencrypted SSH private key for the flynn-test GitHub account, used in TestPrivateSSHKeyClone.
	// Omits header/footer to avoid any GitHub auto-revoke key crawlers
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

	t.Assert(flynn(t, "/", "-a", "gitreceive", "env", "set",
		"SSH_CLIENT_HOSTS=github.com,192.30.252.131 ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAQEAq2A7hRGmdnm9tUDbO9IDSwBK6TbQa+PXYPCPy6rbTrTtw7PHkccKrpp0yVhp5HdEIcKr6pLlVDBfOLX9QUsyCOV0wzfjIJNlGEYsdlLJizHhbn2mUjvSAHQqZETYP81eFzLQNnPHt4EVVUh7VfDESU84KezmD5QlWpXLmvU31/yMf+Se8xhHTvKSCZIFImWwoG6mbUoWf9nzpIoaSjB+weqqUUmpaaasXVal72J+UX2B+2RPW3RcT0eOzQgqlJL3RKrTJvdsjE3JEAvGq3lGHSZXy28G3skua2SmVi/w4yCE6gbODqnTWlg7+wC604ydGXA8VJiS5ap43JXiUFFAaQ==",
		fmt.Sprintf("SSH_CLIENT_KEY=-----BEGIN RSA PRIVATE KEY-----\n%s\n-----END RSA PRIVATE KEY-----\n", sshKey)),
		Succeeds)
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
	t.Assert(push, SuccessfulOutputContains, "bar")
}

func (s *GitDeploySuite) TestEmptyRelease(t *c.C) {
	r := s.newGitRepo(t, "empty-release")
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.flynn("env", "set", "BUILDPACK_URL=https://github.com/kr/heroku-buildpack-inline"), Succeeds)

	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)

	run := r.flynn("run", "echo", "foo")
	t.Assert(run, Succeeds)
	t.Assert(run, Outputs, "foo\n")
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
	t.Assert(push, SuccessfulOutputContains, "cached: 0")

	r.git("commit", "-m", "bump", "--allow-empty")
	push = r.git("push", "flynn", "master")
	t.Assert(push, SuccessfulOutputContains, "cached: 1")
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

func (s *GitDeploySuite) TestPlayBuildpack(t *c.C) {
	s.runBuildpackTest(t, "play-flynn-example", nil)
}

func (s *GitDeploySuite) TestPythonBuildpack(t *c.C) {
	s.runBuildpackTest(t, "python-flynn-example", nil)
}

func (s *GitDeploySuite) TestStaticBuildpack(t *c.C) {
	s.runBuildpackTestWithResponsePattern(t, "static-flynn-example", nil, `Hello, Flynn!`)
}

func (s *GitDeploySuite) runBuildpackTest(t *c.C, name string, resources []string) {
	s.runBuildpackTestWithResponsePattern(t, name, resources, `Hello from Flynn on port \d+`)
}

func (s *GitDeploySuite) runBuildpackTestWithResponsePattern(t *c.C, name string, resources []string, pat string) {
	r := s.newGitRepo(t, "https://github.com/flynn-examples/"+name)

	t.Assert(r.flynn("create", name), Outputs, fmt.Sprintf("Created %s\n", name))

	for _, resource := range resources {
		t.Assert(r.flynn("resource", "add", resource), Succeeds)
	}

	watcher, err := s.controllerClient(t).WatchJobEvents(name, "")
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	push := r.git("push", "flynn", "master")
	t.Assert(push, SuccessfulOutputContains, "Creating release")
	t.Assert(push, SuccessfulOutputContains, "Application deployed")
	t.Assert(push, SuccessfulOutputContains, "Waiting for web job to start...")
	t.Assert(push, SuccessfulOutputContains, "* [new branch]      master -> master")
	t.Assert(push, c.Not(OutputContains), "timed out waiting for scale")
	t.Assert(push, SuccessfulOutputContains, "=====> Default web formation scaled to 1")

	watcher.WaitFor(ct.JobEvents{"web": {"up": 1}}, scaleTimeout, nil)

	route := name + ".dev"
	newRoute := r.flynn("route", "add", "http", route)
	t.Assert(newRoute, Succeeds)

	err = Attempts.Run(func() error {
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
		m, err := regexp.MatchString(pat, string(contents))
		if err != nil {
			return err
		}
		if !m {
			return fmt.Errorf("Expected `%s`, got `%v`", pat, string(contents))
		}
		return nil
	})
	t.Assert(err, c.IsNil)

	t.Assert(r.flynn("scale", "web=0"), Succeeds)
}

func (s *GitDeploySuite) TestRunQuoting(t *c.C) {
	r := s.newGitRepo(t, "empty")
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	run := r.flynn("run", "bash", "-c", "echo 'foo bar'")
	t.Assert(run, Succeeds)
	t.Assert(run, Outputs, "foo bar\n")
}

func (s *GitDeploySuite) TestGitSubmodules(t *c.C) {
	r := s.newGitRepo(t, "empty")
	t.Assert(r.git("submodule", "add", "https://github.com/flynn-examples/go-flynn-example.git"), Succeeds)

	// use a private SSH URL to test ssh client key
	gmPath := filepath.Join(r.dir, ".gitmodules")
	gm, err := ioutil.ReadFile(gmPath)
	t.Assert(err, c.IsNil)
	gm = bytes.Replace(gm, []byte("https://github.com/"), []byte("git@github.com:"), 1)
	err = ioutil.WriteFile(gmPath, gm, os.ModePerm)
	t.Assert(err, c.IsNil)

	t.Assert(r.git("commit", "-am", "Add Submodule"), Succeeds)
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)
	t.Assert(r.flynn("run", "ls", "go-flynn-example"), SuccessfulOutputContains, "main.go")
}

func (s *GitDeploySuite) TestPrivateSSHKeyClone(t *c.C) {
	r := s.newGitRepo(t, "empty-release")
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.flynn("env", "set", "BUILDPACK_URL=git@github.com:kr/heroku-buildpack-inline.git"), Succeeds)

	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
}

// TestConfigDir ensures we don't regress on a bug where uploaded repos were
// being checked out into the bare git repo, which would fail if the repo
// contained a config directory because the bare repo had a config file in it.
func (s *GitDeploySuite) TestConfigDir(t *c.C) {
	r := s.newGitRepo(t, "config-dir")
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)
}
