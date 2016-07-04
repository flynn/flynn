package main

import (
	"path/filepath"
	"strings"

	c "github.com/flynn/go-check"
)

type RedisSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&RedisSuite{})

func (s *RedisSuite) TestDumpRestore(t *c.C) {
	a := s.newCliTestApp(t)

	res := a.flynn("resource", "add", "redis")
	t.Assert(res, Succeeds)
	id := strings.Split(res.Output, " ")[2]

	release, err := s.controllerClient(t).GetAppRelease(a.id)
	t.Assert(err, c.IsNil)

	t.Assert(release.Env["FLYNN_REDIS"], c.Not(c.Equals), "")
	a.waitForService(release.Env["FLYNN_REDIS"])

	t.Assert(a.flynn("redis", "redis-cli", "set", "foo", "bar"), Succeeds)

	file := filepath.Join(t.MkDir(), "dump.rdb")
	t.Assert(a.flynn("redis", "dump", "-f", file), Succeeds)
	t.Assert(a.flynn("redis", "redis-cli", "del", "foo"), Succeeds)

	a.flynn("redis", "restore", "-f", file)

	query := a.flynn("redis", "redis-cli", "get", "foo")
	t.Assert(query, SuccessfulOutputContains, "bar")

	t.Assert(a.flynn("resource", "remove", "redis", id), Succeeds)
}
