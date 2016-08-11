package main

import (
	"time"

	c "github.com/flynn/go-check"
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

func (s *GitreceiveSuite) TestSlugbuilderLimit(t *c.C) {
	r := s.newGitRepo(t, "slugbuilder-limit")
	t.Assert(r.flynn("create"), Succeeds)
	t.Assert(r.flynn("env", "set", "BUILDPACK_URL=git@github.com:kr/heroku-buildpack-inline.git"), Succeeds)
	t.Assert(r.flynn("-a", "gitreceive", "env", "set", "SLUGBUILDER_DEFAULT_MEMORY_LIMIT=2GB"), Succeeds)

	push := r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, OutputContains, "2147483648")

	t.Assert(r.flynn("limit", "set", "slugbuilder", "memory=500MB"), Succeeds)

	t.Assert(r.git("commit", "-m", "bump", "--allow-empty"), Succeeds)
	push = r.git("push", "flynn", "master")
	t.Assert(push, Succeeds)
	t.Assert(push, OutputContains, "524288000")

	t.Assert(r.flynn("-a", "gitreceive", "env", "unset", "SLUGBUILDER_DEFAULT_MEMORY_LIMIT"), Succeeds)
}
