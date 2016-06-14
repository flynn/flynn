package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/testutils/postgres"
	"github.com/flynn/flynn/router/types"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

func setupTestDB(c *C, dbname string) *postgres.DB {
	if err := pgtestutils.SetupPostgres(dbname); err != nil {
		c.Fatal(err)
	}
	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     os.Getenv("PGHOST"),
			Database: dbname,
		},
	})
	if err != nil {
		c.Fatal(err)
	}
	return postgres.New(pgxpool, nil)
}

type MigrateSuite struct{}

var _ = Suite(&MigrateSuite{})

type testMigrator struct {
	c  *C
	db *postgres.DB
	id int
}

func (t *testMigrator) migrateTo(id int) {
	t.c.Assert((*migrations)[t.id:id].Migrate(t.db), IsNil)
	t.id = id
}

func (MigrateSuite) TestMigrateTLSObject(c *C) {
	db := setupTestDB(c, "routertest_tls_object_migration")
	m := &testMigrator{c: c, db: db}

	// start from ID 4
	m.migrateTo(4)

	nRoutes := 3
	routes := make([]*router.Route, nRoutes)
	cert := tlsConfigForDomain("migrationtest.example.com")
	for i := 0; i < nRoutes-1; i++ {
		r := &router.Route{
			ParentRef:     fmt.Sprintf("some/parent/ref/%d", i+1),
			Service:       fmt.Sprintf("migrationtest%d.example.com", i+1),
			Domain:        fmt.Sprintf("migrationtest%d.example.com", i+1),
			LegacyTLSCert: cert.Cert,
			LegacyTLSKey:  cert.PrivateKey,
		}
		err := db.QueryRow(`
			INSERT INTO http_routes (parent_ref, service, domain, tls_cert, tls_key)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			r.ParentRef,
			r.Service,
			r.Domain,
			r.LegacyTLSCert,
			r.LegacyTLSKey).Scan(&r.ID)
		c.Assert(err, IsNil)
		routes[i] = r
	}

	{
		// Add route with leading and trailing whitespace on cert and key
		i := len(routes) - 1
		r := &router.Route{
			ParentRef:     fmt.Sprintf("some/parent/ref/%d", i+1),
			Service:       fmt.Sprintf("migrationtest%d.example.com", i+1),
			Domain:        fmt.Sprintf("migrationtest%d.example.com", i+1),
			LegacyTLSCert: "  \n\n  \n " + cert.Cert + "   \n   \n   ",
			LegacyTLSKey:  "    \n   " + cert.PrivateKey + "   \n   \n  ",
		}
		err := db.QueryRow(`
			INSERT INTO http_routes (parent_ref, service, domain, tls_cert, tls_key)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			r.ParentRef,
			r.Service,
			r.Domain,
			r.LegacyTLSCert,
			r.LegacyTLSKey).Scan(&r.ID)
		c.Assert(err, IsNil)
		routes[i] = r
	}

	// run TLS object migration
	m.migrateTo(5)

	certSHA256 := hex.EncodeToString(sha256.New().Sum([]byte(cert.Cert)))

	for _, r := range routes {
		fetchedRoute := &router.Route{}
		fetchedCert := &router.Certificate{}
		var fetchedCertSHA256 string
		err := db.QueryRow(`
			SELECT r.parent_ref, r.service, r.domain, c.cert, c.key, c.cert_sha256 FROM http_routes AS r
			INNER JOIN route_certificates AS rc ON rc.http_route_id = r.id
			INNER JOIN certificates AS c ON rc.certificate_id = c.id
			WHERE r.id = $1
		`, r.ID).Scan(&fetchedRoute.ParentRef, &fetchedRoute.Service, &fetchedRoute.Domain, &fetchedCert.Cert, &fetchedCert.Key, &fetchedCertSHA256)
		c.Assert(err, IsNil)

		c.Assert(fetchedRoute.ParentRef, Equals, r.ParentRef)
		c.Assert(fetchedRoute.Service, Equals, r.Service)
		c.Assert(fetchedRoute.Domain, Equals, r.Domain)
		c.Assert(fetchedCert.Cert, Equals, cert.Cert)
		c.Assert(fetchedCert.Key, Equals, cert.PrivateKey)
		c.Assert(fetchedCertSHA256, Equals, certSHA256)
	}

	var count int64
	err := db.QueryRow(`SELECT COUNT(*) FROM certificates`).Scan(&count)
	c.Assert(err, IsNil)
	c.Assert(count, Equals, int64(1))

	err = db.QueryRow(`SELECT COUNT(*) FROM route_certificates`).Scan(&count)
	c.Assert(err, IsNil)
	c.Assert(count, Equals, int64(nRoutes))
}
