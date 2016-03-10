package main

import (
	"fmt"
	"path/filepath"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
)

type PostgresSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&PostgresSuite{})

// Check postgres config to avoid regressing on https://github.com/flynn/flynn/issues/101
func (s *PostgresSuite) TestSSLRenegotiationLimit(t *c.C) {
	query := flynn(t, "/", "-a", "controller", "pg", "psql", "--", "-c", "SHOW ssl_renegotiation_limit")
	t.Assert(query, SuccessfulOutputContains, "ssl_renegotiation_limit \n-------------------------\n 0\n(1 row)")
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
	t.Assert(query, SuccessfulOutputContains, "foobar")
}

var sireniaPostgres = sireniaDatabase{
	appName:    "postgres",
	serviceKey: "FLYNN_POSTGRES",
	hostKey:    "PGHOST",
	initDb: func(t *c.C, r *ct.Release, d *sireniaDeploy) {
		db := postgres.Wait(&postgres.Conf{
			Service:  d.name,
			User:     "flynn",
			Password: r.Env["PGPASSWORD"],
			Database: "postgres",
		}, nil)
		dbname := "deploy-test"
		t.Assert(db.Exec(fmt.Sprintf(`CREATE DATABASE "%s" WITH OWNER = "flynn"`, dbname)), c.IsNil)
		db.Close()
		db = postgres.Wait(&postgres.Conf{
			Service:  d.name,
			User:     "flynn",
			Password: r.Env["PGPASSWORD"],
			Database: dbname,
		}, nil)
		defer db.Close()
		t.Assert(db.Exec(`CREATE TABLE deploy_test ( data text)`), c.IsNil)
	},
	assertWriteable: func(t *c.C, r *ct.Release, d *sireniaDeploy) {
		dbname := "deploy-test"
		db := postgres.Wait(&postgres.Conf{
			Service:  d.name,
			User:     "flynn",
			Password: r.Env["PGPASSWORD"],
			Database: dbname,
		}, nil)
		defer db.Close()
		debug(t, "writing to postgres database")
		t.Assert(db.ExecRetry(`INSERT INTO deploy_test (data) VALUES ('data')`), c.IsNil)
	},
}

// Sirenia integration tests
func (s *PostgresSuite) TestDeploySingleAsync(t *c.C) {
	testSireniaDeploy(s.controllerClient(t), s.discoverdClient(t), t, &sireniaDeploy{
		name:        "postgres-single-async",
		db:          sireniaPostgres,
		sireniaJobs: 3,
		webJobs:     2,
		expected:    testDeploySingleAsync,
	})
}

func (s *PostgresSuite) TestDeployMultipleAsync(t *c.C) {
	testSireniaDeploy(s.controllerClient(t), s.discoverdClient(t), t, &sireniaDeploy{
		name:        "postgres-multiple-async",
		db:          sireniaPostgres,
		sireniaJobs: 5,
		webJobs:     2,
		expected:    testDeployMultipleAsync,
	})
}
