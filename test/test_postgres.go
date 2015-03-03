package main

import (
	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

type PostgresSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&PostgresSuite{})

// Check postgres config to avoid regressing on https://github.com/flynn/flynn/issues/101
func (s *PostgresSuite) TestSSLRenegotiationLimit(t *c.C) {
	query := flynn(t, "/", "-a", "controller", "psql", "--", "-c", "SHOW ssl_renegotiation_limit")
	t.Assert(query, Succeeds)
	t.Assert(query, OutputContains, "ssl_renegotiation_limit \n-------------------------\n 0\n(1 row)")
}
