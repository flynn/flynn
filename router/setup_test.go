package main

import (
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flynn/flynn/discoverd/cache"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/testutil"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/testutils/postgres"
	"github.com/flynn/flynn/router/schema"
	"github.com/flynn/flynn/router/types"
	. "github.com/flynn/go-check"
	"github.com/jackc/pgx"
)

func init() {
	listenFunc = net.Listen
}

type discoverdClient interface {
	DiscoverdClient
	AddServiceAndRegister(string, string) (discoverd.Heartbeater, error)
}

// discoverdWrapper wraps a discoverd client to expose Close method that closes
// all heartbeaters
type discoverdWrapper struct {
	discoverdClient
	hbs []io.Closer
}

func (d *discoverdWrapper) AddServiceAndRegister(service, addr string) (discoverd.Heartbeater, error) {
	hb, err := d.discoverdClient.AddServiceAndRegister(service, addr)
	if err != nil {
		return nil, err
	}
	d.hbs = append(d.hbs, hb)
	return hb, nil
}

func (d *discoverdWrapper) Cleanup() {
	for _, hb := range d.hbs {
		hb.Close()
	}
	d.hbs = nil
}

func setup(t testutil.TestingT) (*discoverdWrapper, func()) {
	dc, killDiscoverd := testutil.BootDiscoverd(t, "")
	dw := &discoverdWrapper{discoverdClient: dc}

	return dw, func() {
		killDiscoverd()
	}
}

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
	discoverd *discoverdWrapper
	cleanup   func()
	pgx       *pgx.ConnPool
}

var _ = Suite(&S{})

const dbname = "routertest"

func (s *S) SetUpSuite(c *C) {
	s.discoverd, s.cleanup = setup(c)

	if err := pgtestutils.SetupPostgres(dbname); err != nil {
		c.Fatal(err)
	}
	pgxConfig := newPgxConnPoolConfig()
	pgxpool, err := pgx.NewConnPool(pgxConfig)
	if err != nil {
		c.Fatal(err)
	}
	db := postgres.New(pgxpool, nil)

	if err = migrateDB(db); err != nil {
		c.Fatal(err)
	}
	db.Close()

	// reconnect with prepared statements
	pgxConfig.AfterConnect = schema.PrepareStatements
	pgxpool, err = pgx.NewConnPool(pgxConfig)
	if err != nil {
		c.Fatal(err)
	}
	db = postgres.New(pgxpool, nil)

	s.pgx = db.ConnPool
	s.pgx.Exec(sqlCreateTruncateTables)
}

func newPgxConnPoolConfig() pgx.ConnPoolConfig {
	return pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     os.Getenv("PGHOST"),
			Database: dbname,
		},
	}
}

func (s *S) TearDownSuite(c *C) {
	s.cleanup()
}

func (s *S) TearDownTest(c *C) {
	s.discoverd.Cleanup()
	s.pgx.Exec("SELECT truncate_tables()")
}

const waitTimeout = time.Second

func waitForEvent(c *C, w Watcher, event string, id string) func() *router.Event {
	ch := make(chan *router.Event)
	w.Watch(ch)
	return func() *router.Event {
		defer w.Unwatch(ch)
		for {
			timeout := time.After(waitTimeout)
			select {
			case e := <-ch:
				if e.Event == event && (id == "" || e.ID == id) {
					return e
				}
			case <-timeout:
				c.Fatalf("timeout exceeded waiting for %s %s", event, id)
				return nil
			}
		}
	}
}

func discoverdRegisterTCP(c *C, l *TCPListener, addr string) func() {
	return discoverdRegisterTCPService(c, l, "test", addr)
}

func discoverdRegisterTCPService(c *C, l *TCPListener, name, addr string) func() {
	dc := l.discoverd.(discoverdClient)
	sc := l.services[name].sc
	return discoverdRegister(c, dc, sc, name, addr)
}

func discoverdRegisterHTTP(c *C, l *HTTPListener, addr string) func() {
	return discoverdRegisterHTTPService(c, l, "test", addr)
}

func discoverdRegisterHTTPService(c *C, l *HTTPListener, name, addr string) func() {
	dc := l.discoverd.(discoverdClient)
	sc := l.services[name].sc
	return discoverdRegister(c, dc, sc, name, addr)
}

func discoverdSetLeaderHTTP(c *C, l *HTTPListener, name, id string) {
	dc := l.discoverd.(discoverdClient)
	sc := l.services[name].sc
	discoverdSetLeader(c, dc, sc, name, id)
}

func discoverdSetLeaderTCP(c *C, l *TCPListener, name, id string) {
	dc := l.discoverd.(discoverdClient)
	sc := l.services[name].sc
	discoverdSetLeader(c, dc, sc, name, id)
}

func discoverdSetLeader(c *C, dc discoverdClient, sc *cache.ServiceCache, name, id string) {
	done := make(chan struct{})
	go func() {
		events := make(chan *discoverd.Event)
		stream := sc.Watch(events, true)
		defer stream.Close()
		for event := range events {
			if event.Kind == discoverd.EventKindLeader && event.Instance.ID == id {
				close(done)
				return
			}
		}
	}()
	err := dc.Service(name).SetLeader(id)
	c.Assert(err, IsNil)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		c.Fatal("timed out waiting for discoverd leader change")
	}
}

func discoverdRegister(c *C, dc discoverdClient, sc *cache.ServiceCache, name, addr string) func() {
	done := make(chan struct{})
	go func() {
		events := make(chan *discoverd.Event)
		stream := sc.Watch(events, true)
		defer stream.Close()
		for event := range events {
			if event.Kind == discoverd.EventKindUp && event.Instance.Addr == addr {
				close(done)
				return
			}
		}
	}()
	hb, err := dc.AddServiceAndRegister(name, addr)
	c.Assert(err, IsNil)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		c.Fatal("timed out waiting for discoverd registration")
	}
	return discoverdUnregisterFunc(c, hb, sc)
}

func discoverdUnregisterFunc(c *C, hb discoverd.Heartbeater, sc *cache.ServiceCache) func() {
	return func() {
		done := make(chan struct{})
		started := make(chan struct{})
		go func() {
			events := make(chan *discoverd.Event)
			stream := sc.Watch(events, false)
			defer stream.Close()
			close(started)
			for event := range events {
				if event.Kind == discoverd.EventKindDown && event.Instance.Addr == hb.Addr() {
					close(done)
					return
				}
			}
		}()
		<-started
		c.Assert(hb.Close(), IsNil)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			c.Fatal("timed out waiting for discoverd unregister")
		}
	}
}

func addRoute(c *C, l Listener, r *router.Route) *router.Route {
	wait := waitForEvent(c, l, "set", "")
	err := l.AddRoute(r)
	c.Assert(err, IsNil)
	wait()
	return r
}

func addRouteAssertErr(c *C, l Listener, r *router.Route) error {
	err := l.AddRoute(r)
	c.Assert(err, NotNil)
	return err
}

const sqlCreateTruncateTables = `
CREATE OR REPLACE FUNCTION truncate_tables() RETURNS void AS $$
DECLARE
    statements CURSOR FOR
        SELECT tablename FROM pg_tables
        WHERE tablename != 'schema_migrations'
          AND tableowner = session_user
          AND schemaname = 'public';
BEGIN
    FOR stmt IN statements LOOP
        EXECUTE 'TRUNCATE TABLE ' || quote_ident(stmt.tablename) || ' CASCADE;';
    END LOOP;
END;
$$ LANGUAGE plpgsql;
`

func removeRoute(c *C, l Listener, id string) {
	wait := waitForEvent(c, l, "remove", "")
	err := l.RemoveRoute(id)
	c.Assert(err, IsNil)
	wait()
}

func removeRouteAssertErr(c *C, l Listener, id string) error {
	err := l.RemoveRoute(id)
	c.Assert(err, NotNil)
	return err
}

var portAlloc uint32 = 4500

func allocatePort() int {
	return int(atomic.AddUint32(&portAlloc, 1))
}

func allocatePortRange(count int) (int, int) {
	max := int(atomic.AddUint32(&portAlloc, uint32(count)))
	return max - (count - 1), max
}
