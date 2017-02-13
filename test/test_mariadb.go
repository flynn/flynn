package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flynn/flynn/appliance/mariadb"
	ct "github.com/flynn/flynn/controller/types"
	c "github.com/flynn/go-check"
	_ "github.com/go-sql-driver/mysql"
)

type MariaDBSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&MariaDBSuite{})

// Sirenia integration tests
var sireniaMariaDB = sireniaDatabase{
	appName:    "mariadb",
	serviceKey: "FLYNN_MYSQL",
	hostKey:    "MYSQL_HOST",
	initDb: func(t *c.C, r *ct.Release, d *sireniaFormation) {
		dsn := &mariadb.DSN{
			Host:     fmt.Sprintf("leader.%s.discoverd", d.name) + ":3306",
			User:     "flynn",
			Password: r.Env["MYSQL_PWD"],
			Database: "mysql",
		}
		db, err := sql.Open("mysql", dsn.String())
		t.Assert(err, c.IsNil)
		defer db.Close()
		dbname := "deploy_test"
		_, err = db.Exec(fmt.Sprintf(`CREATE DATABASE %s`, dbname))
		t.Assert(err, c.IsNil)
		_, err = db.Exec(fmt.Sprintf(`USE %s`, dbname))
		t.Assert(err, c.IsNil)
		_, err = db.Exec(`CREATE TABLE deploy_test (data TEXT)`)
		t.Assert(err, c.IsNil)
	},
	assertWriteable: func(t *c.C, r *ct.Release, d *sireniaFormation) {
		dbname := "deploy_test"
		dsn := &mariadb.DSN{
			Host:     fmt.Sprintf("leader.%s.discoverd", d.name) + ":3306",
			User:     "flynn",
			Password: r.Env["MYSQL_PWD"],
			Database: dbname,
		}
		db, err := sql.Open("mysql", dsn.String())
		t.Assert(err, c.IsNil)
		defer db.Close()
		_, err = db.Exec(`INSERT INTO deploy_test (data) VALUES ('data')`)
		t.Assert(err, c.IsNil)
	},
}

func (s *MariaDBSuite) TestDeploySingleAsync(t *c.C) {
	testSireniaDeploy(s.controllerClient(t), s.discoverdClient(t), t, &sireniaFormation{
		name:        "mariadb-single-async",
		db:          sireniaMariaDB,
		sireniaJobs: 3,
		webJobs:     2,
	}, testDeploySingleAsync)
}

func (s *MariaDBSuite) TestDeployMultipleAsync(t *c.C) {
	testSireniaDeploy(s.controllerClient(t), s.discoverdClient(t), t, &sireniaFormation{
		name:        "mariadb-multiple-async",
		db:          sireniaMariaDB,
		sireniaJobs: 5,
		webJobs:     2,
	}, testDeployMultipleAsync)
}

func (s *MariaDBSuite) TestDumpRestore(t *c.C) {
	r := s.newGitRepo(t, "empty")
	t.Assert(r.flynn("create"), Succeeds)

	res := r.flynn("resource", "add", "mysql")
	t.Assert(res, Succeeds)
	id := strings.Split(res.Output, " ")[2]

	t.Assert(r.flynn("mysql", "console", "--", "-e",
		"CREATE TABLE T (F text); INSERT INTO T (F) VALUES ('abc')"), Succeeds)

	file := filepath.Join(t.MkDir(), "db.dump")
	t.Assert(r.flynn("mysql", "dump", "-f", file), Succeeds)
	t.Assert(r.flynn("mysql", "console", "--", "-e", "DROP TABLE T"), Succeeds)

	r.flynn("mysql", "restore", "-f", file)

	query := r.flynn("mysql", "console", "--", "-e", "SELECT * FROM T")
	t.Assert(query, SuccessfulOutputContains, "abc")

	t.Assert(r.flynn("resource", "remove", "mysql", id), Succeeds)
}

func (s *MariaDBSuite) TestTunables(t *c.C) {
	testSireniaTunables(s.controllerClient(t), s.discoverdClient(t), t, &sireniaFormation{
		name:        "mariadb-tunables",
		db:          sireniaMariaDB,
		sireniaJobs: 3,
		webJobs:     2,
	}, []tunableTest{
		{"online update", sireniaTunable{"query_cache_limit", "1048576", "2097152"}},
		{"requires restart", sireniaTunable{"innodb_doublewrite", "0", "1"}},
	})
}
