package main

import (
	"path/filepath"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

type PostgresSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&PostgresSuite{})

// Check postgres config to avoid regressing on https://github.com/flynn/flynn/issues/101
func (s *PostgresSuite) TestSSLRenegotiationLimit(t *c.C) {
	query := flynn(t, "/", "-a", "controller", "pg", "psql", "--", "-c", "SHOW ssl_renegotiation_limit")
	t.Assert(query, Succeeds)
	t.Assert(query, OutputContains, "ssl_renegotiation_limit \n-------------------------\n 0\n(1 row)")
}

func (s *PostgresSuite) TestDumpRestore(t *c.C) {
	r := s.newGitRepo(t, "empty")
	t.Assert(r.flynn("create"), Succeeds)

	t.Assert(r.flynn("resource", "add", "postgres"), Succeeds)

	t.Assert(r.flynn("pg", "psql", "--", "-c",
		"CREATE table foos (data text); INSERT INTO foos (data) VALUES ('foobar')"), Succeeds)

	file := filepath.Join(t.MkDir(), "db.dump")
	t.Assert(r.flynn("pg", "dump", "-f", file), Succeeds)
	t.Assert(r.flynn("pg", "psql", "--", "-c", "DROP TABLE foos"), Succeeds)

	r.flynn("pg", "restore", "-f", file)

	query := r.flynn("pg", "psql", "--", "-c", "SELECT * FROM foos")
	t.Assert(query, Succeeds)
	t.Assert(query, OutputContains, "foobar")
}
