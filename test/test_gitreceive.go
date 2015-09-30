package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

type GitreceiveSuite struct {
	Helper
}

var _ = c.Suite(&GitreceiveSuite{})

func (s *GitreceiveSuite) SetUpSuite(t *c.C) {
	// Unencrypted SSH private key for the flynn-test GitHub account.
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

func (s *GitreceiveSuite) TestRepoCaching(t *c.C) {
	r := s.newGitRepo(t, "empty")
	t.Assert(r.flynn("create"), Succeeds)

	r.git("commit", "-m", "bump", "--allow-empty")
	r.git("commit", "-m", "bump", "--allow-empty")
	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, c.Not(OutputContains), "cached")

	// cycle the receiver to clear any cache
	t.Assert(flynn(t, "/", "-a", "gitreceive", "scale", "app=0"), Succeeds)
	t.Assert(flynn(t, "/", "-a", "gitreceive", "scale", "app=1"), Succeeds)
	_, err := s.discoverdClient(t).Instances("gitreceive", 10*time.Second)
	t.Assert(err, c.IsNil)

	r.git("commit", "-m", "bump", "--allow-empty")
	push = r.git("push", "flynn", "master", "--progress")
	// should only contain one object
	t.Assert(push, SuccessfulOutputContains, "Counting objects: 1, done.")
}

func (s *GitreceiveSuite) TestPrivateSSHKeyClone(t *c.C) {
	r := s.newGitRepo(t, "private-clone")
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.flynn("env", "set", "BUILDPACK_URL=git@github.com:kr/heroku-buildpack-inline.git"), Succeeds)

	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
}

func (s *GitreceiveSuite) TestGitSubmodules(t *c.C) {
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
