package main

import (
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

type GitreceiveSuite struct {
	Helper
}

var _ = c.Suite(&GitreceiveSuite{})

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
