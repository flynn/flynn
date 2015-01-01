package main

import (
	"bytes"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/pkg/exec"
)

type PostgresSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&PostgresSuite{})

// Check postgres config to avoid regressing on https://github.com/flynn/flynn/issues/101
func (s *PostgresSuite) TestSSLRenegotiationLimit(t *c.C) {
	services, err := s.discoverdClient(t).Services("pg", 5*time.Second)
	t.Assert(err, c.IsNil)

	cmd := exec.Command(exec.DockerImage(imageURIs["postgresql"]),
		"--tuples-only", "--command", "show ssl_renegotiation_limit;")
	cmd.Entrypoint = []string{"psql"}
	cmd.Env = map[string]string{
		"PGDATABASE": "postgres",
		"PGHOST":     services[0].Host,
		"PGPORT":     services[0].Port,
		"PGUSER":     services[0].Attrs["username"],
		"PGPASSWORD": services[0].Attrs["password"],
	}

	res, err := cmd.CombinedOutput()
	t.Assert(err, c.IsNil)
	t.Assert(string(bytes.TrimSpace(res)), c.Equals, "0")
}
