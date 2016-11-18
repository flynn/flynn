package postgresql

import (
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/sirenia/state"
	. "github.com/flynn/go-check"
	"github.com/jackc/pgx"
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
		Port:      "6500",
		OpTimeout: 30 * time.Second,
	}

	p := NewProcess(cfg)
	err := p.Reconfigure(&state.Config{Role: state.RolePrimary})
	c.Assert(err, IsNil)

	err = p.Start()
	c.Assert(err, IsNil)
	defer p.Stop()

	conn := connect(c, p, "postgres")
	_, err = conn.Exec("CREATE DATABASE test")
	conn.Close()
	c.Assert(err, IsNil)

	err = p.Stop()
	c.Assert(err, IsNil)

	// ensure that we can start a new instance from the same directory
	p = NewProcess(cfg)
	err = p.Reconfigure(&state.Config{Role: state.RolePrimary})
	c.Assert(err, IsNil)
	c.Assert(p.Start(), IsNil)
	defer p.Stop()

	conn = connect(c, p, "test")
	_, err = conn.Exec("CREATE DATABASE foo")
	conn.Close()
	c.Assert(err, IsNil)

	err = p.Stop()
	c.Assert(err, IsNil)
}

func instance(p *Process) *discoverd.Instance {
	return &discoverd.Instance{
		ID:   p.id,
		Addr: "127.0.0.1:" + p.port,
		Meta: map[string]string{IDKey: p.id},
	}
}

var newPort uint32 = 6510

func NewTestProcess(c *C, n int) *Process {
	return NewProcess(Config{
		ID:        fmt.Sprintf("node%d", n),
		DataDir:   c.MkDir(),
		Port:      strconv.Itoa(int(atomic.AddUint32(&newPort, 1))),
		OpTimeout: 30 * time.Second,
	})
}

func connect(c *C, p *Process, db string) *pgx.Conn {
	port, _ := strconv.Atoi(p.port)
	conn, err := pgx.Connect(pgx.ConnConfig{
		Host:     "127.0.0.1",
		Port:     uint16(port),
		User:     "flynn",
		Password: "password",
		Database: db,
	})
	c.Assert(err, IsNil)
	return conn
}

func pgConfig(role state.Role, upstream, downstream *Process) *state.Config {
	c := &state.Config{Role: role}
	if upstream != nil {
		c.Upstream = instance(upstream)
	}
	if downstream != nil {
		c.Downstream = instance(downstream)
	}
	return c
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
	c.Assert(err, IsNil, Commentf("up:%s down:%s", p.id, id))
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
	node1 := NewTestProcess(c, 1) // primary
	node2 := NewTestProcess(c, 2) // sync
	node3 := NewTestProcess(c, 3) // async
	node4 := NewTestProcess(c, 4) // second async

	// Start a primary
	err := node1.Reconfigure(pgConfig(state.RolePrimary, nil, node2))
	c.Assert(err, IsNil)
	c.Assert(node1.Start(), IsNil)
	defer node1.Stop()

	// try to write to primary and make sure it's read-only
	node1Conn := connect(c, node1, "postgres")
	defer node1Conn.Close()
	_, err = node1Conn.Exec("CREATE DATABASE foo")
	c.Assert(err, NotNil)
	c.Assert(err.(pgx.PgError).Code, Equals, "25006") // can't write while read only

	// Start a sync
	err = node2.Reconfigure(pgConfig(state.RoleSync, node1, node3))
	c.Assert(err, IsNil)
	c.Assert(node2.Start(), IsNil)
	defer node2.Stop()

	// check it catches up
	waitReplSync(c, node1, 2)

	// try to query primary until it comes up as read-write
	waitReadWrite(c, node1Conn)

	for _, n := range []*Process{node1, node2} {
		pos, err := n.XLogPosition()
		c.Assert(err, IsNil)
		c.Assert(pos, Not(Equals), "")
		c.Assert(pos, Not(Equals), n.XLog().Zero())
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
	node2Conn := connect(c, node2, "postgres")
	defer node2Conn.Close()
	waitRow(c, node2Conn, 1)
	assertRecovery(c, node2Conn)

	// Start an async
	err = node3.Reconfigure(pgConfig(state.RoleAsync, node2, node4))
	c.Assert(err, IsNil)
	c.Assert(node3.Start(), IsNil)
	defer node3.Stop()

	// check it catches up
	waitReplSync(c, node2, 3)

	node3Conn := connect(c, node3, "postgres")
	defer node3Conn.Close()

	// check that data replicated successfully
	waitRow(c, node3Conn, 1)
	assertRecovery(c, node3Conn)
	assertDownstream(c, node2Conn, 3)

	// Start a second async
	err = node4.Reconfigure(pgConfig(state.RoleAsync, node3, nil))
	c.Assert(err, IsNil)
	c.Assert(node4.Start(), IsNil)
	defer node4.Stop()

	// check it catches up
	waitReplSync(c, node3, 4)

	node4Conn := connect(c, node4, "postgres")
	defer node4Conn.Close()

	// check that data replicated successfully
	waitRow(c, node4Conn, 1)
	assertRecovery(c, node4Conn)
	assertDownstream(c, node3Conn, 4)

	// promote node2 to primary
	c.Assert(node1.Stop(), IsNil)
	err = node2.Reconfigure(pgConfig(state.RolePrimary, nil, node3))
	c.Assert(err, IsNil)
	err = node3.Reconfigure(pgConfig(state.RoleSync, node2, node4))
	c.Assert(err, IsNil)

	// wait for recovery and read-write transactions to come up
	waitRecovered(c, node2Conn)
	waitReplSync(c, node2, 3)
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
	err = node3.Reconfigure(pgConfig(state.RolePrimary, nil, node4))
	c.Assert(err, IsNil)
	err = node4.Reconfigure(pgConfig(state.RoleSync, node3, nil))
	c.Assert(err, IsNil)

	// check replication
	waitRecovered(c, node3Conn)
	waitReplSync(c, node3, 4)
	waitReadWrite(c, node3Conn)
	assertDownstream(c, node3Conn, 4)
	insertRow(c, node3Conn, 3)
}

func (PostgresSuite) TestRemoveNodes(c *C) {
	// start a chain of four nodes
	node1 := NewTestProcess(c, 1)
	node2 := NewTestProcess(c, 2)
	node3 := NewTestProcess(c, 3)
	node4 := NewTestProcess(c, 4)
	err := node1.Reconfigure(pgConfig(state.RolePrimary, nil, node2))
	c.Assert(err, IsNil)
	c.Assert(node1.Start(), IsNil)
	defer node1.Stop()

	err = node2.Reconfigure(pgConfig(state.RoleSync, node1, nil))
	c.Assert(err, IsNil)
	c.Assert(node2.Start(), IsNil)
	defer node2.Stop()

	err = node3.Reconfigure(pgConfig(state.RoleAsync, node2, nil))
	c.Assert(err, IsNil)
	c.Assert(node3.Start(), IsNil)
	defer node3.Stop()

	err = node4.Reconfigure(pgConfig(state.RoleAsync, node3, nil))
	c.Assert(err, IsNil)
	c.Assert(node4.Start(), IsNil)
	defer node4.Stop()

	// wait for cluster to come up
	node1Conn := connect(c, node1, "postgres")
	defer node1Conn.Close()
	node4Conn := connect(c, node4, "postgres")
	defer node4Conn.Close()
	waitReadWrite(c, node1Conn)
	createTable(c, node1Conn)
	waitRow(c, node4Conn, 1)
	node4Conn.Close()

	// remove first async
	c.Assert(node3.Stop(), IsNil)
	// reconfigure second async
	err = node4.Reconfigure(pgConfig(state.RoleAsync, node2, nil))
	c.Assert(err, IsNil)
	// run query
	node4Conn = connect(c, node4, "postgres")
	defer node4Conn.Close()
	insertRow(c, node1Conn, 2)
	waitRow(c, node4Conn, 2)
	node4Conn.Close()

	// remove sync and promote node4 to sync
	c.Assert(node2.Stop(), IsNil)
	err = node1.Reconfigure(pgConfig(state.RolePrimary, nil, node4))
	c.Assert(err, IsNil)
	err = node4.Reconfigure(pgConfig(state.RoleSync, node1, nil))
	c.Assert(err, IsNil)

	waitReadWrite(c, node1Conn)
	insertRow(c, node1Conn, 3)
	node4Conn = connect(c, node4, "postgres")
	defer node4Conn.Close()
	waitRow(c, node4Conn, 3)
}
