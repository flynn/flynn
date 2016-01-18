package main

import (
	"path/filepath"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

type RedisSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&RedisSuite{})

func (s *RedisSuite) TestDumpRestore(t *c.C) {
	r := s.newGitRepo(t, "empty")
	t.Assert(r.flynn("create"), Succeeds)

	t.Assert(r.flynn("resource", "add", "redis"), Succeeds)

	t.Assert(r.flynn("redis", "redis-cli", "set", "foo", "bar"), Succeeds)

	file := filepath.Join(t.MkDir(), "dump.rdb")
	t.Assert(r.flynn("redis", "dump", "-f", file), Succeeds)
	t.Assert(r.flynn("redis", "redis-cli", "del", "foo"), Succeeds)

	r.flynn("redis", "restore", "-f", file)

	query := r.flynn("redis", "redis-cli", "get", "foo")
	t.Assert(query, SuccessfulOutputContains, "bar")
}
