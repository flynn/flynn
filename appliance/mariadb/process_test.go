package mariadb

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-sql-driver/mysql"
	"github.com/flynn/flynn/appliance/postgresql/state"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type MariaDBSuite struct{}

var _ = Suite(&MariaDBSuite{})

func (MariaDBSuite) TestSingletonPrimary(c *C) {
	p := NewProcess()
	p.ID = "node1"
	p.Singleton = true
	p.Password = "password"
	p.DataDir = c.MkDir()
	p.Port = "54320"
	p.OpTimeout = 30 * time.Second
	err := p.Reconfigure(&state.PgConfig{Role: state.RolePrimary})
	c.Assert(err, IsNil)

	err = p.Start()
	c.Assert(err, IsNil)
	defer p.Stop()

	conn := connect(c, 0, "")
	_, err = conn.Exec("CREATE DATABASE test")
	conn.Close()
	c.Assert(err, IsNil)

	err = p.Stop()
	c.Assert(err, IsNil)

	// ensure that we can start a new instance from the same directory
	p = NewProcess()
	p.ID = "node1"
	p.Singleton = true
	p.Password = "password"
	p.DataDir = c.MkDir()
	p.Port = "54320"
	p.OpTimeout = 30 * time.Second
	err = p.Reconfigure(&state.PgConfig{Role: state.RolePrimary})
	c.Assert(err, IsNil)
	c.Assert(p.Start(), IsNil)
	defer p.Stop()

	conn = connect(c, 0, "")
	_, err = conn.Exec("CREATE DATABASE foo")
	conn.Close()
	c.Assert(err, IsNil)

	err = p.Stop()
	c.Assert(err, IsNil)
}

func instance(n int) *discoverd.Instance {
	id := fmt.Sprintf("node%d", n)
	return &discoverd.Instance{
		ID:   id,
		Addr: fmt.Sprintf("127.0.0.1:5432%d", n),
		Meta: map[string]string{"MYSQL_ID": id},
	}
}

func connect(c *C, n int, database string) *sql.DB {
	dsn := DSN{
		Host:     fmt.Sprintf("127.0.0.1:%d", 54320+uint16(n)),
		User:     "flynn",
		Password: "password",
		Database: database,
	}
	println("DBG.DSN", dsn.String())
	db, err := sql.Open("mysql", dsn.String())
	c.Assert(err, IsNil)
	return db
}

func pgConfig(role state.Role, upstream, downstream int) *state.PgConfig {
	c := &state.PgConfig{Role: role}
	if upstream > 0 {
		c.Upstream = instance(upstream)
	}
	if downstream > 0 {
		c.Downstream = instance(downstream)
	}
	return c
}

var queryAttempts = attempt.Strategy{
	Min:   5,
	Total: 30 * time.Second,
	Delay: 200 * time.Millisecond,
}

func assertDownstream(c *C, db *sql.DB, n int) {
	var res string
	err := db.QueryRow("SELECT client_addr FROM pg_stat_replication WHERE application_name = $1", fmt.Sprintf("node%d", n)).Scan(&res)
	c.Assert(err, IsNil)
}

func assertRecovery(c *C, db *sql.DB) {
	var recovery bool
	err := db.QueryRow("SELECT pg_is_in_recovery()").Scan(&recovery)
	c.Assert(err, IsNil)
	c.Assert(recovery, Equals, true)
}

func waitRow(c *C, db *sql.DB, n int) {
	var res int64
	err := queryAttempts.Run(func() error {
		return db.QueryRow("SELECT id FROM test WHERE id = $1", n).Scan(&res)
	})
	c.Assert(err, IsNil)
}

func createTable(c *C, db *sql.DB) {
	_, err := db.Exec("CREATE TABLE test (id bigint PRIMARY KEY)")
	c.Assert(err, IsNil)
	insertRow(c, db, 1)
}

func insertRow(c *C, db *sql.DB, n int) {
	_, err := db.Exec("INSERT INTO test (id) VALUES ($1)", n)
	c.Assert(err, IsNil)
}

func waitReadWrite(c *C, db *sql.DB) {
	var readOnly string
	err := queryAttempts.Run(func() error {
		if err := db.QueryRow("SHOW default_transaction_read_only").Scan(&readOnly); err != nil {
			return err
		}
		if readOnly == "off" {
			return nil
		}
		return fmt.Errorf("transaction readonly is %q", readOnly)
	})
	c.Assert(err, IsNil)
}

var syncAttempts = attempt.Strategy{
	Min:   5,
	Total: 30 * time.Second,
	Delay: 200 * time.Millisecond,
}

func waitReplSync(c *C, p *Process, n int) {
	id := fmt.Sprintf("node%d", n)
	err := syncAttempts.Run(func() error {
		info, err := p.Info()
		if err != nil {
			return err
		}
		if info.SyncedDownstream == nil || info.SyncedDownstream.ID != id {
			return errors.New("downstream not synced")
		}
		return nil
	})
	c.Assert(err, IsNil, Commentf("up:%s down:%s", p.ID, id))
}

func waitRecovered(c *C, db *sql.DB) {
	var recovery bool
	err := queryAttempts.Run(func() error {
		err := db.QueryRow("SELECT pg_is_in_recovery()").Scan(&recovery)
		if err != nil {
			return err
		}
		if recovery {
			return fmt.Errorf("in recovery")
		}
		return nil
	})
	c.Assert(err, IsNil)
}

func (MariaDBSuite) TestIntegration(c *C) {
	// Start a primary
	node1 := NewTestProcess(c, 1)
	err := node1.Reconfigure(pgConfig(state.RolePrimary, 0, 2))
	c.Assert(err, IsNil)
	c.Assert(node1.Start(), IsNil)
	defer node1.Stop()

	// try to write to primary and make sure it's read-only
	db1 := connect(c, 1, "mysql")
	defer db1.Close()
	_, err = db1.Exec("CREATE DATABASE foo")
	c.Assert(err, NotNil)
	// c.Assert(err.(pgx.PgError).Code, Equals, "25006") // can't write while read only

	// Start a sync
	node2 := NewTestProcess(c, 2)
	err = node2.Reconfigure(pgConfig(state.RoleSync, 1, 3))
	c.Assert(err, IsNil)
	c.Assert(node2.Start(), IsNil)
	defer node2.Stop()

	// check it catches up
	waitReplSync(c, node1, 2)

	// try to query primary until it comes up as read-write
	waitReadWrite(c, db1)

	for _, n := range []*Process{node1, node2} {
		pos, err := n.XLogPosition()
		c.Assert(err, IsNil)
		c.Assert(pos, Not(Equals), "")
		c.Assert(pos, Not(Equals), "master-bin.000000/0")
	}

	// make sure the sync is listed as sync and remote_write is enabled
	assertDownstream(c, db1, 2)
	var res string
	err = db1.QueryRow("SHOW synchronous_standby_names").Scan(&res)
	c.Assert(err, IsNil)
	c.Assert(res, Equals, "node2")
	err = db1.QueryRow("SHOW synchronous_commit").Scan(&res)
	c.Assert(err, IsNil)
	c.Assert(res, Equals, "remote_write")

	// create a table and a row
	createTable(c, db1)
	db1.Close()

	// query the sync and see the database
	db2 := connect(c, 2, "mysql")
	defer db2.Close()
	waitRow(c, db2, 1)
	assertRecovery(c, db2)

	// Start an async
	node3 := NewTestProcess(c, 3)
	err = node3.Reconfigure(pgConfig(state.RoleAsync, 2, 4))
	c.Assert(err, IsNil)
	c.Assert(node3.Start(), IsNil)
	defer node3.Stop()

	// check it catches up
	waitReplSync(c, node2, 3)

	db3 := connect(c, 3, "mysql")
	defer db3.Close()

	// check that data replicated successfully
	waitRow(c, db3, 1)
	assertRecovery(c, db3)
	assertDownstream(c, db2, 3)

	// Start a second async
	node4 := NewTestProcess(c, 4)
	err = node4.Reconfigure(pgConfig(state.RoleAsync, 3, 0))
	c.Assert(err, IsNil)
	c.Assert(node4.Start(), IsNil)
	defer node4.Stop()

	// check it catches up
	waitReplSync(c, node3, 4)

	db4 := connect(c, 4, "mysql")
	defer db4.Close()

	// check that data replicated successfully
	waitRow(c, db4, 1)
	assertRecovery(c, db4)
	assertDownstream(c, db3, 4)

	// promote node2 to primary
	c.Assert(node1.Stop(), IsNil)
	err = node2.Reconfigure(pgConfig(state.RolePrimary, 0, 3))
	c.Assert(err, IsNil)
	err = node3.Reconfigure(pgConfig(state.RoleSync, 2, 4))
	c.Assert(err, IsNil)

	// wait for recovery and read-write transactions to come up
	waitRecovered(c, db2)
	waitReplSync(c, node2, 3)
	waitReadWrite(c, db2)

	// check replication of each node
	assertDownstream(c, db2, 3)
	assertDownstream(c, db3, 4)

	// write to primary and ensure data propagates to followers
	insertRow(c, db2, 2)
	db2.Close()
	waitRow(c, db3, 2)
	waitRow(c, db4, 2)

	//  promote node3 to primary
	c.Assert(node2.Stop(), IsNil)
	err = node3.Reconfigure(pgConfig(state.RolePrimary, 0, 4))
	c.Assert(err, IsNil)
	err = node4.Reconfigure(pgConfig(state.RoleSync, 3, 0))

	// check replication
	waitRecovered(c, db3)
	waitReplSync(c, node3, 4)
	waitReadWrite(c, db3)
	assertDownstream(c, db3, 4)
	insertRow(c, db3, 3)
}

func (MariaDBSuite) TestRemoveNodes(c *C) {
	// start a chain of four nodes
	node1 := NewTestProcess(c, 1)
	err := node1.Reconfigure(pgConfig(state.RolePrimary, 0, 2))
	c.Assert(err, IsNil)
	c.Assert(node1.Start(), IsNil)
	defer node1.Stop()

	node2 := NewTestProcess(c, 2)
	err = node2.Reconfigure(pgConfig(state.RoleSync, 1, 0))
	c.Assert(err, IsNil)
	c.Assert(node2.Start(), IsNil)
	defer node2.Stop()

	node3 := NewTestProcess(c, 3)
	err = node3.Reconfigure(pgConfig(state.RoleAsync, 2, 0))
	c.Assert(err, IsNil)
	c.Assert(node3.Start(), IsNil)
	defer node3.Stop()

	node4 := NewTestProcess(c, 4)
	err = node4.Reconfigure(pgConfig(state.RoleAsync, 3, 0))
	c.Assert(err, IsNil)
	c.Assert(node4.Start(), IsNil)
	defer node4.Stop()

	// wait for cluster to come up
	node1Conn := connect(c, 1, "mysql")
	defer node1Conn.Close()
	db4 := connect(c, 4, "mysql")
	defer db4.Close()
	waitReadWrite(c, node1Conn)
	createTable(c, node1Conn)
	waitRow(c, db4, 1)
	db4.Close()

	// remove first async
	c.Assert(node3.Stop(), IsNil)
	// reconfigure second async
	err = node4.Reconfigure(pgConfig(state.RoleAsync, 2, 0))
	c.Assert(err, IsNil)
	// run query
	db4 = connect(c, 4, "mysql")
	defer db4.Close()
	insertRow(c, node1Conn, 2)
	waitRow(c, db4, 2)
	db4.Close()

	// remove sync and promote node4 to sync
	c.Assert(node2.Stop(), IsNil)
	err = node1.Reconfigure(pgConfig(state.RolePrimary, 0, 4))
	c.Assert(err, IsNil)
	err = node4.Reconfigure(pgConfig(state.RoleSync, 1, 0))
	c.Assert(err, IsNil)

	waitReadWrite(c, node1Conn)
	insertRow(c, node1Conn, 3)
	db4 = connect(c, 4, "mysql")
	defer db4.Close()
	waitRow(c, db4, 3)
}

func NewTestProcess(c *C, n int) *Process {
	p := NewProcess()
	p.ID = fmt.Sprintf("node%d", n)
	p.DataDir = c.MkDir()
	p.Port = fmt.Sprintf("5432%d", n)
	p.Password = "password"
	p.OpTimeout = 30 * time.Second
	return p
}
