package main

import (
	"fmt"
	"testing"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/appliance/postgresql/state"
	"github.com/flynn/flynn/appliance/postgresql/xlog"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type PostgresSuite struct{}

var _ = Suite(&PostgresSuite{})

func (PostgresSuite) TestSingletonPrimary(c *C) {
	cfg := Config{
		ID:        "node1",
		Singleton: true,
		DataDir:   c.MkDir(),
		Port:      "54320",
		OpTimeout: 30 * time.Second,
	}

	pg := NewPostgres(cfg)
	err := pg.Reconfigure(&state.PgConfig{Role: state.RolePrimary})
	c.Assert(err, IsNil)

	err = pg.Start()
	c.Assert(err, IsNil)
	defer pg.Stop()

	conn := connect(c, 0, "postgres")
	_, err = conn.Exec("CREATE DATABASE test")
	conn.Close()
	c.Assert(err, IsNil)

	err = pg.Stop()
	c.Assert(err, IsNil)

	// ensure that we can start a new instance from the same directory
	pg = NewPostgres(cfg)
	err = pg.Reconfigure(&state.PgConfig{Role: state.RolePrimary})
	c.Assert(err, IsNil)
	c.Assert(pg.Start(), IsNil)
	defer pg.Stop()

	conn = connect(c, 0, "test")
	_, err = conn.Exec("CREATE DATABASE foo")
	conn.Close()
	c.Assert(err, IsNil)

	err = pg.Stop()
	c.Assert(err, IsNil)
}

func instance(n int) *discoverd.Instance {
	return &discoverd.Instance{
		ID:   fmt.Sprintf("node%d", n),
		Addr: fmt.Sprintf("127.0.0.1:5432%d", n),
	}
}

func newPostgres(c *C, n int) state.Postgres {
	return NewPostgres(Config{
		ID:        fmt.Sprintf("node%d", n),
		DataDir:   c.MkDir(),
		Port:      fmt.Sprintf("5432%d", n),
		OpTimeout: 30 * time.Second,
	})
}

func connect(c *C, n int, db string) *pgx.Conn {
	conn, err := pgx.Connect(pgx.ConnConfig{
		Host:     "127.0.0.1",
		Port:     54320 + uint16(n),
		User:     "flynn",
		Password: "password",
		Database: db,
	})
	c.Assert(err, IsNil)
	return conn
}

func pgConfig(role state.Role, n int) *state.PgConfig {
	var inst *discoverd.Instance
	if n > 0 {
		inst = instance(n)
	}
	if role == state.RolePrimary {
		return &state.PgConfig{Role: role, Downstream: inst}
	}
	return &state.PgConfig{Role: role, Upstream: inst}
}

var queryAttempts = attempt.Strategy{
	Min:   5,
	Total: 30 * time.Second,
	Delay: 200 * time.Millisecond,
}

func assertDownstream(c *C, conn *pgx.Conn, n int) {
	var res string
	err := conn.QueryRow("SELECT client_addr FROM pg_stat_replication WHERE application_name = $1", fmt.Sprintf("node%d", n)).Scan(&res)
	c.Assert(err, IsNil)
}

func assertRecovery(c *C, conn *pgx.Conn) {
	var recovery bool
	err := conn.QueryRow("SELECT pg_is_in_recovery()").Scan(&recovery)
	c.Assert(err, IsNil)
	c.Assert(recovery, Equals, true)
}

func waitRow(c *C, conn *pgx.Conn, n int) {
	var res int64
	err := queryAttempts.Run(func() error {
		return conn.QueryRow("SELECT id FROM test WHERE id = $1", n).Scan(&res)
	})
	c.Assert(err, IsNil)
}

func createTable(c *C, conn *pgx.Conn) {
	_, err := conn.Exec("CREATE TABLE test (id bigint PRIMARY KEY)")
	c.Assert(err, IsNil)
	insertRow(c, conn, 1)
}

func insertRow(c *C, conn *pgx.Conn, n int) {
	_, err := conn.Exec("INSERT INTO test (id) VALUES ($1)", n)
	c.Assert(err, IsNil)
}

func waitReadWrite(c *C, conn *pgx.Conn) {
	var readOnly string
	err := queryAttempts.Run(func() error {
		if err := conn.QueryRow("SHOW default_transaction_read_only").Scan(&readOnly); err != nil {
			return err
		}
		if readOnly == "off" {
			return nil
		}
		return fmt.Errorf("transaction readonly is %q", readOnly)
	})
	c.Assert(err, IsNil)
}

func waitRecovered(c *C, conn *pgx.Conn) {
	var recovery bool
	err := queryAttempts.Run(func() error {
		err := conn.QueryRow("SELECT pg_is_in_recovery()").Scan(&recovery)
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

func (PostgresSuite) TestIntegration(c *C) {
	// Start a primary
	node1 := newPostgres(c, 1)
	err := node1.Reconfigure(pgConfig(state.RolePrimary, 2))
	c.Assert(err, IsNil)
	c.Assert(node1.Start(), IsNil)
	defer node1.Stop()

	// try to write to primary and make sure it's read-only
	node1Conn := connect(c, 1, "postgres")
	defer node1Conn.Close()
	_, err = node1Conn.Exec("CREATE DATABASE foo")
	c.Assert(err, NotNil)
	c.Assert(err.(pgx.PgError).Code, Equals, "25006") // can't write while read only

	// Start a sync
	node2 := newPostgres(c, 2)
	err = node2.Reconfigure(pgConfig(state.RoleSync, 1))
	c.Assert(err, IsNil)
	c.Assert(node2.Start(), IsNil)
	defer node2.Stop()

	// try to query primary until it comes up as read-write
	waitReadWrite(c, node1Conn)

	for _, n := range []state.Postgres{node1, node2} {
		pos, err := n.XLogPosition()
		c.Assert(err, IsNil)
		c.Assert(pos, Not(Equals), "")
		c.Assert(pos, Not(Equals), xlog.Zero)
	}

	// make sure the sync is listed as sync and remote_write is enabled
	assertDownstream(c, node1Conn, 2)
	var res string
	err = node1Conn.QueryRow("SHOW synchronous_standby_names").Scan(&res)
	c.Assert(err, IsNil)
	c.Assert(res, Equals, "node2")
	err = node1Conn.QueryRow("SHOW synchronous_commit").Scan(&res)
	c.Assert(err, IsNil)
	c.Assert(res, Equals, "remote_write")

	// create a table and a row
	createTable(c, node1Conn)
	node1Conn.Close()

	// query the sync and see the database
	node2Conn := connect(c, 2, "postgres")
	defer node2Conn.Close()
	waitRow(c, node2Conn, 1)
	assertRecovery(c, node2Conn)

	// Start an async
	node3 := newPostgres(c, 3)
	err = node3.Reconfigure(pgConfig(state.RoleAsync, 2))
	c.Assert(err, IsNil)
	c.Assert(node3.Start(), IsNil)
	defer node3.Stop()

	node3Conn := connect(c, 3, "postgres")
	defer node3Conn.Close()

	// check that data replicated successfully
	waitRow(c, node3Conn, 1)
	assertRecovery(c, node3Conn)
	assertDownstream(c, node2Conn, 3)

	// Start a second async
	node4 := newPostgres(c, 4)
	err = node4.Reconfigure(pgConfig(state.RoleAsync, 3))
	c.Assert(err, IsNil)
	c.Assert(node4.Start(), IsNil)
	defer node4.Stop()

	node4Conn := connect(c, 4, "postgres")
	defer node4Conn.Close()

	// check that data replicated successfully
	waitRow(c, node4Conn, 1)
	assertRecovery(c, node4Conn)
	assertDownstream(c, node3Conn, 4)

	// promote node2 to primary
	c.Assert(node1.Stop(), IsNil)
	err = node2.Reconfigure(pgConfig(state.RolePrimary, 3))
	c.Assert(err, IsNil)
	err = node3.Reconfigure(pgConfig(state.RoleSync, 2))
	c.Assert(err, IsNil)

	// wait for recovery and read-write transactions to come up
	waitRecovered(c, node2Conn)
	waitReadWrite(c, node2Conn)

	// check replication of each node
	assertDownstream(c, node2Conn, 3)
	assertDownstream(c, node3Conn, 4)

	// write to primary and ensure data propagates to followers
	insertRow(c, node2Conn, 2)
	node2Conn.Close()
	waitRow(c, node3Conn, 2)
	waitRow(c, node4Conn, 2)

	//  promote node3 to primary
	c.Assert(node2.Stop(), IsNil)
	err = node3.Reconfigure(pgConfig(state.RolePrimary, 4))
	c.Assert(err, IsNil)
	err = node4.Reconfigure(pgConfig(state.RoleSync, -1))

	// check replication
	waitRecovered(c, node3Conn)
	waitReadWrite(c, node3Conn)
	assertDownstream(c, node3Conn, 4)
	insertRow(c, node3Conn, 3)
}

func (PostgresSuite) TestRemoveNodes(c *C) {
	// start a chain of four nodes
	node1 := newPostgres(c, 1)
	err := node1.Reconfigure(pgConfig(state.RolePrimary, 2))
	c.Assert(err, IsNil)
	c.Assert(node1.Start(), IsNil)
	defer node1.Stop()

	node2 := newPostgres(c, 2)
	err = node2.Reconfigure(pgConfig(state.RoleSync, 1))
	c.Assert(err, IsNil)
	c.Assert(node2.Start(), IsNil)
	defer node2.Stop()

	node3 := newPostgres(c, 3)
	err = node3.Reconfigure(pgConfig(state.RoleAsync, 2))
	c.Assert(err, IsNil)
	c.Assert(node3.Start(), IsNil)
	defer node3.Stop()

	node4 := newPostgres(c, 4)
	err = node4.Reconfigure(pgConfig(state.RoleAsync, 3))
	c.Assert(err, IsNil)
	c.Assert(node4.Start(), IsNil)
	defer node4.Stop()

	// wait for cluster to come up
	node1Conn := connect(c, 1, "postgres")
	defer node1Conn.Close()
	node4Conn := connect(c, 4, "postgres")
	defer node4Conn.Close()
	waitReadWrite(c, node1Conn)
	createTable(c, node1Conn)
	waitRow(c, node4Conn, 1)
	node4Conn.Close()

	// remove first async
	c.Assert(node3.Stop(), IsNil)
	// reconfigure second async
	err = node4.Reconfigure(pgConfig(state.RoleAsync, 2))
	c.Assert(err, IsNil)
	// run query
	node4Conn = connect(c, 4, "postgres")
	defer node4Conn.Close()
	insertRow(c, node1Conn, 2)
	waitRow(c, node4Conn, 2)
	node4Conn.Close()

	// remove sync and promote node4 to sync
	c.Assert(node2.Stop(), IsNil)
	err = node1.Reconfigure(pgConfig(state.RolePrimary, 4))
	c.Assert(err, IsNil)
	err = node4.Reconfigure(pgConfig(state.RoleSync, 1))
	c.Assert(err, IsNil)

	waitReadWrite(c, node1Conn)
	insertRow(c, node1Conn, 3)
	node4Conn = connect(c, 4, "postgres")
	defer node4Conn.Close()
	waitRow(c, node4Conn, 3)
}
