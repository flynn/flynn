package controllergrpc

import (
	fmt "fmt"
	"log"
	"net"
	"testing"

	"github.com/flynn/flynn/controller/app"
	"github.com/flynn/flynn/controller/database"
	tu "github.com/flynn/flynn/controller/testutils"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/testutils/postgres"
	. "github.com/flynn/go-check"
	"github.com/jackc/pgx"
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
	cc  *tu.FakeCluster
	lis net.Listener
	hc  *Config
	c   ControllerClient
}

var _ = Suite(&S{})

func newTestClient(endpoint string) (ControllerClient, error) {
	conn, err := grpc.Dial(endpoint, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	return NewControllerClient(conn), nil
}

func newLocalListener() net.Listener {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if l, err = net.Listen("tcp6", "[::1]:0"); err != nil {
			panic(fmt.Sprintf("httptest: failed to listen on a port: %v", err))
		}
	}
	return l
}

func newTestServer(c *Config) net.Listener {
	s := NewServer(c)
	lis := newLocalListener()
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()
	return lis
}

func (s *S) SetUpSuite(c *C) {
	dbname := "controllergrpctest"
	db, err := pgtestutils.SetupAndConnectPostgres(dbname)
	if err != nil {
		c.Fatal(err)
	}
	if err := database.MigrateDB(db); err != nil {
		c.Fatal(err)
	}
	db.Close()

	// reconnect with statements prepared now that schema is migrated

	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     "/var/run/postgresql",
			Database: dbname,
		},
		AfterConnect: database.PrepareStatements,
	})
	if err != nil {
		c.Fatal(err)
	}
	db = postgres.New(pgxpool, nil)

	s.cc = tu.NewFakeCluster()
	s.hc = &Config{
		DB:           db,
		RouterClient: tu.NewFakeRouter(),
	}

	s.lis = newTestServer(s.hc)
	client, err := newTestClient(s.lis.Addr().String())
	c.Assert(err, IsNil)
	s.c = client
}

func (s *S) SetUpTest(c *C) {
	s.cc.SetHosts(make(map[string]utils.HostClient))
}

func (s *S) createTestApp(c *C, in *ct.App) *ct.App {
	r := apprepo.NewRepo(s.hc.DB, "", nil)
	c.Assert(r.Add(in), IsNil)
	return in
}

func (s *S) TestListApps(c *C) {
	appName := "list-test"
	s.createTestApp(c, &ct.App{Name: appName})
	resp, err := s.c.ListApps(context.Background(), &ListAppsRequest{})
	c.Assert(err, IsNil)

	c.Assert(resp.Apps, NotNil)
	c.Assert(len(resp.Apps) > 0, Equals, true)
	c.Assert(resp.Apps[0].Name, Not(Equals), "")
	c.Assert(resp.Apps[0].DisplayName, Equals, appName)
}
