package data

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	pgtestutils "github.com/flynn/flynn/pkg/testutils/postgres"
	"github.com/flynn/flynn/pkg/tlscert"
	"github.com/flynn/flynn/router/testutils"
	router "github.com/flynn/flynn/router/types"
	"github.com/jackc/pgx"

	. "github.com/flynn/go-check"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

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

// TestMigrateJobStates checks that migrating to ID 9 does not break existing
// job records
func (MigrateSuite) TestMigrateJobStates(c *C) {
	db := setupTestDB(c, "controllertest_migrate_job_states")
	m := &testMigrator{c: c, db: db}

	// start from ID 7
	m.migrateTo(7)

	// insert a job
	hostID := "host1"
	uuid := random.UUID()
	jobID := cluster.GenerateJobID(hostID, uuid)
	appID := random.UUID()
	releaseID := random.UUID()
	c.Assert(db.Exec(`INSERT INTO apps (app_id, name) VALUES ($1, $2)`, appID, "migrate-app"), IsNil)
	c.Assert(db.Exec(`INSERT INTO releases (release_id) VALUES ($1)`, releaseID), IsNil)
	c.Assert(db.Exec(`INSERT INTO job_cache (job_id, app_id, release_id, state) VALUES ($1, $2, $3, $4)`, jobID, appID, releaseID, "up"), IsNil)

	// migrate to 8 and check job states are still constrained
	m.migrateTo(8)
	err := db.Exec(`UPDATE job_cache SET state = 'foo' WHERE job_id = $1`, jobID)
	c.Assert(err, NotNil)
	if !postgres.IsPostgresCode(err, postgres.ForeignKeyViolation) {
		c.Fatalf("expected postgres foreign key violation, got %s", err)
	}

	// migrate to 9 and check job IDs are correct, pending state is valid
	m.migrateTo(9)
	var clusterID, dbUUID, dbHostID string
	c.Assert(db.QueryRow("SELECT cluster_id, job_id, host_id FROM job_cache WHERE cluster_id = $1", jobID).Scan(&clusterID, &dbUUID, &dbHostID), IsNil)
	c.Assert(clusterID, Equals, jobID)
	c.Assert(dbUUID, Equals, uuid)
	c.Assert(dbHostID, Equals, hostID)
	c.Assert(db.Exec(`UPDATE job_cache SET state = 'pending' WHERE job_id = $1`, uuid), IsNil)
}

func (MigrateSuite) TestMigrateCriticalApps(c *C) {
	db := setupTestDB(c, "controllertest_migrate_critical_apps")
	m := &testMigrator{c: c, db: db}

	// start from ID 12
	m.migrateTo(12)

	// create the critical apps with system app meta
	criticalApps := []string{"discoverd", "flannel", "postgres", "controller"}
	meta := map[string]string{"flynn-system-app": "true"}
	for _, name := range criticalApps {
		c.Assert(db.Exec(`INSERT INTO apps (app_id, name, meta) VALUES ($1, $2, $3)`, random.UUID(), name, meta), IsNil)
	}

	// migrate to 13 and check critical app meta was updated
	m.migrateTo(13)
	for _, name := range criticalApps {
		var meta map[string]string
		c.Assert(db.QueryRow("SELECT meta FROM apps WHERE name = $1", name).Scan(&meta), IsNil)
		c.Assert(meta["flynn-system-app"], Equals, "true")
		c.Assert(meta["flynn-system-critical"], Equals, "true")
	}
}

// TestMigrateReleaseArtifacts checks that migrating to ID 15 correctly
// migrates releases by creating appropriate records in the release_artifacts
// table
func (MigrateSuite) TestMigrateReleaseArtifacts(c *C) {
	db := setupTestDB(c, "controllertest_migrate_release_artifacts")
	m := &testMigrator{c: c, db: db}

	// start from ID 14
	m.migrateTo(14)

	// add some artifacts and releases
	releaseArtifacts := map[string]string{
		random.UUID(): random.UUID(),
		random.UUID(): random.UUID(),
		random.UUID(): random.UUID(),
	}
	for releaseID, artifactID := range releaseArtifacts {
		c.Assert(db.Exec(`INSERT INTO artifacts (artifact_id, type, uri) VALUES ($1, $2, $3)`, artifactID, "docker", "http://example.com/"+artifactID), IsNil)
		c.Assert(db.Exec(`INSERT INTO releases (release_id, artifact_id) VALUES ($1, $2)`, releaseID, artifactID), IsNil)
	}
	c.Assert(db.Exec(`INSERT INTO releases (release_id) VALUES ($1)`, random.UUID()), IsNil)

	// insert multiple slug based releases with the same slug URI
	slugReleaseIDs := []string{random.UUID(), random.UUID()}
	imageArtifactID := random.UUID()
	slugEnv := map[string]string{"SLUG_URL": "http://example.com/slug.tgz"}
	c.Assert(db.Exec(`INSERT INTO artifacts (artifact_id, type, uri) VALUES ($1, $2, $3)`, imageArtifactID, "docker", "http://example.com/"+imageArtifactID), IsNil)
	for _, id := range slugReleaseIDs {
		c.Assert(db.Exec(`INSERT INTO releases (release_id, artifact_id, env) VALUES ($1, $2, $3)`, id, imageArtifactID, slugEnv), IsNil)
		releaseArtifacts[id] = imageArtifactID
	}

	// migrate to 15 and check release_artifacts was populated correctly
	m.migrateTo(15)
	rows, err := db.Query("SELECT release_id, artifact_id FROM release_artifacts INNER JOIN artifacts USING (artifact_id) WHERE type = 'docker'")
	c.Assert(err, IsNil)
	defer rows.Close()
	actual := make(map[string]string)
	for rows.Next() {
		var releaseID, artifactID string
		c.Assert(rows.Scan(&releaseID, &artifactID), IsNil)
		actual[releaseID] = artifactID
	}
	c.Assert(rows.Err(), IsNil)
	c.Assert(actual, DeepEquals, releaseArtifacts)

	for _, id := range slugReleaseIDs {
		// check the slug releases got "git=true" in metadata
		var releaseMeta map[string]string
		err = db.QueryRow("SELECT meta FROM releases WHERE release_id = $1", id).Scan(&releaseMeta)
		c.Assert(err, IsNil)
		c.Assert(releaseMeta, DeepEquals, map[string]string{"git": "true"})

		// check the slug releases got a file artifact with the correct URI and meta
		var slugURI string
		var artifactMeta map[string]string
		err = db.QueryRow("SELECT uri, meta FROM artifacts INNER JOIN release_artifacts USING (artifact_id) WHERE type = 'file' AND release_id = $1", id).Scan(&slugURI, &artifactMeta)
		c.Assert(err, IsNil)
		c.Assert(slugURI, Equals, slugEnv["SLUG_URL"])
		c.Assert(artifactMeta, DeepEquals, map[string]string{"blobstore": "true"})
	}
}

// TestMigrateArtifactMeta checks that migrating to ID 16 correctly
// sets artifact metadata for those stored in the blobstore
func (MigrateSuite) TestMigrateArtifactMeta(c *C) {
	db := setupTestDB(c, "controllertest_migrate_artifact_meta")
	m := &testMigrator{c: c, db: db}

	// start from ID 15
	m.migrateTo(15)

	type artifact struct {
		ID         string
		URI        string
		MetaBefore map[string]string
		MetaAfter  map[string]string
	}

	artifacts := []*artifact{
		{
			ID:         random.UUID(),
			URI:        "http://example.com/file1.tar",
			MetaBefore: nil,
			MetaAfter:  nil,
		},
		{
			ID:         random.UUID(),
			URI:        "http://example.com/file2.tar",
			MetaBefore: map[string]string{"foo": "bar"},
			MetaAfter:  map[string]string{"foo": "bar"},
		},
		{
			ID:         random.UUID(),
			URI:        "http://blobstore.discoverd/file1.tar",
			MetaBefore: nil,
			MetaAfter:  map[string]string{"blobstore": "true"},
		},
		{
			ID:         random.UUID(),
			URI:        "http://blobstore.discoverd/file2.tar",
			MetaBefore: map[string]string{"foo": "bar"},
			MetaAfter:  map[string]string{"foo": "bar", "blobstore": "true"},
		},
	}

	// create the artifacts
	for _, a := range artifacts {
		c.Assert(db.Exec(`INSERT INTO artifacts (artifact_id, type, uri, meta) VALUES ($1, $2, $3, $4)`, a.ID, "file", a.URI, a.MetaBefore), IsNil)
	}

	// migrate to 16 and check the artifacts have the appropriate metadata
	m.migrateTo(16)
	for _, a := range artifacts {
		var meta map[string]string
		c.Assert(db.QueryRow("SELECT meta FROM artifacts WHERE artifact_id = $1", a.ID).Scan(&meta), IsNil)
		c.Assert(meta, DeepEquals, a.MetaAfter)
	}
}

func (MigrateSuite) TestMigrateReleaseArtifactIndex(c *C) {
	db := setupTestDB(c, "controllertest_migrate_release_artifact_index")
	m := &testMigrator{c: c, db: db}

	// start from ID 16
	m.migrateTo(16)

	// create some releases and artifacts
	releaseIDs := []string{random.UUID(), random.UUID()}
	for _, releaseID := range releaseIDs {
		c.Assert(db.Exec(`INSERT INTO releases (release_id) VALUES ($1)`, releaseID), IsNil)
	}
	artifactIDs := []string{random.UUID(), random.UUID()}
	c.Assert(db.Exec(`INSERT INTO artifacts (artifact_id, type, uri) VALUES ($1, $2, $3)`, artifactIDs[0], "docker", "http://example.com"), IsNil)
	c.Assert(db.Exec(`INSERT INTO artifacts (artifact_id, type, uri) VALUES ($1, $2, $3)`, artifactIDs[1], "file", "http://example.com"), IsNil)

	// insert some rows into release_artifacts
	for _, releaseID := range releaseIDs {
		for _, artifactID := range artifactIDs {
			c.Assert(db.Exec(`INSERT INTO release_artifacts (release_id, artifact_id) VALUES ($1, $2)`, releaseID, artifactID), IsNil)
		}
	}

	// migrate to 17 and check the index column was set correctly
	m.migrateTo(17)
	for _, releaseID := range releaseIDs {
		for i, artifactID := range artifactIDs {
			var index int32
			c.Assert(db.QueryRow(`SELECT index FROM release_artifacts WHERE release_id = $1 AND artifact_id = $2`, releaseID, artifactID).Scan(&index), IsNil)
			c.Assert(index, Equals, int32(i))
		}
	}
}

func (MigrateSuite) TestMigrateProcessArgs(c *C) {
	db := setupTestDB(c, "controllertest_migrate_process_args")
	m := &testMigrator{c: c, db: db}

	// start from ID 18
	m.migrateTo(18)

	// create some process types with entrypoint / cmd
	type oldProcType struct {
		Entrypoint []string `json:"entrypoint,omitempty"`
		Cmd        []string `json:"cmd,omitempty"`
	}
	releases := map[string]*oldProcType{
		random.UUID(): {},
		random.UUID(): {
			Entrypoint: []string{"sh"},
		},
		random.UUID(): {
			Entrypoint: []string{"sh", "-c", "date"},
		},
		random.UUID(): {
			Cmd: []string{"sh"},
		},
		random.UUID(): {
			Cmd: []string{"sh", "-c", "date"},
		},
		random.UUID(): {
			Entrypoint: []string{"sh"},
			Cmd:        []string{"-c", "date"},
		},
		random.UUID(): {
			Entrypoint: []string{"sh", "-c"},
			Cmd:        []string{"date"},
		},
	}
	for id, proc := range releases {
		procs := map[string]*oldProcType{"web": proc, "app": proc}
		c.Assert(db.Exec(`INSERT INTO releases (release_id, processes) VALUES ($1, $2)`, id, procs), IsNil)
	}

	// create some system apps
	systemMeta := map[string]string{"flynn-system-app": "true"}
	controllerID := random.UUID()
	controllerProcs := map[string]*oldProcType{
		"scheduler": {Cmd: []string{"scheduler"}},
		"web":       {Cmd: []string{"controller"}},
		"worker":    {Cmd: []string{"worker"}},
	}
	c.Assert(db.Exec(`INSERT INTO releases (release_id, processes) VALUES ($1, $2)`, controllerID, controllerProcs), IsNil)
	c.Assert(db.Exec(`INSERT INTO apps (app_id, name, release_id, meta) VALUES ($1, $2, $3, $4)`, random.UUID(), "controller", controllerID, systemMeta), IsNil)
	routerID := random.UUID()
	routerProcs := map[string]*oldProcType{
		"app": {Cmd: []string{"-http-port", "80", "-https-port", "443", "-tcp-range-start", "3000", "-tcp-range-end", "3500"}},
	}
	c.Assert(db.Exec(`INSERT INTO releases (release_id, processes) VALUES ($1, $2)`, routerID, routerProcs), IsNil)
	c.Assert(db.Exec(`INSERT INTO apps (app_id, name, release_id, meta) VALUES ($1, $2, $3, $4)`, random.UUID(), "router", routerID, systemMeta), IsNil)

	// create a slug release
	slugID := random.UUID()
	slugProcs := map[string]*oldProcType{
		"web":    {Cmd: []string{"start web"}},
		"worker": {Cmd: []string{"start worker"}},
	}
	c.Assert(db.Exec(`INSERT INTO releases (release_id, processes, meta) VALUES ($1, $2, $3)`, slugID, slugProcs, map[string]string{"git": "true"}), IsNil)

	// migrate to 19 and check Args was populated correctly
	m.migrateTo(19)
	type newProcType struct {
		Args []string `json:"args,omitempty"`
	}
	for id, proc := range releases {
		var procs map[string]*newProcType
		c.Assert(db.QueryRow(`SELECT processes FROM releases WHERE release_id = $1`, id).Scan(&procs), IsNil)
		for _, typ := range []string{"web", "app"} {
			c.Assert(procs[typ].Args, DeepEquals, append(proc.Entrypoint, proc.Cmd...))
		}
	}

	// check the system + slug apps got the correct entrypoint prepended to Args
	for _, x := range []struct {
		releaseID  string
		oldProcs   map[string]*oldProcType
		entrypoint string
	}{
		{controllerID, controllerProcs, "/bin/start-flynn-controller"},
		{routerID, routerProcs, "/bin/flynn-router"},
		{slugID, slugProcs, "/runner/init"},
	} {
		var procs map[string]*newProcType
		c.Assert(db.QueryRow(`SELECT processes FROM releases WHERE release_id = $1`, x.releaseID).Scan(&procs), IsNil)
		for typ, proc := range x.oldProcs {
			c.Assert(procs[typ].Args, DeepEquals, append([]string{x.entrypoint}, proc.Cmd...))
		}
	}
}

func (MigrateSuite) TestMigrateRedisService(c *C) {
	db := setupTestDB(c, "controllertest_migrate_redis_service")
	m := &testMigrator{c: c, db: db}

	// start from ID 19
	m.migrateTo(19)

	type procType struct {
		Service string `json:"service"`
	}

	// add a Redis app
	appName := "redis-" + random.UUID()
	appMeta := map[string]string{"flynn-system-app": "true"}
	releaseID := random.UUID()
	procs := map[string]*procType{
		"redis": {Service: "redis"},
	}
	c.Assert(db.Exec(`INSERT INTO releases (release_id, processes) VALUES ($1, $2)`, releaseID, procs), IsNil)
	c.Assert(db.Exec(`INSERT INTO apps (app_id, name, release_id, meta) VALUES ($1, $2, $3, $4)`, random.UUID(), appName, releaseID, appMeta), IsNil)

	// migrate to 20 and check the service got updated
	m.migrateTo(20)
	var updatedProcs map[string]*procType
	c.Assert(db.QueryRow(`SELECT processes FROM releases WHERE release_id = $1`, releaseID).Scan(&updatedProcs), IsNil)
	proc, ok := updatedProcs["redis"]
	if !ok {
		c.Fatal("missing redis process type")
	}
	c.Assert(proc.Service, Equals, appName)
}

func (MigrateSuite) TestMigrateDefaultAppGC(c *C) {
	db := setupTestDB(c, "controllertest_migrate_default_app_gc")
	m := &testMigrator{c: c, db: db}

	// start from ID 23
	m.migrateTo(23)

	// add some apps
	type app struct {
		ID      string
		OldMeta map[string]string
		NewMeta map[string]string
	}
	apps := []*app{
		{
			OldMeta: nil,
			NewMeta: map[string]string{"gc.max_inactive_slug_releases": "10"},
		},
		{
			OldMeta: map[string]string{},
			NewMeta: map[string]string{"gc.max_inactive_slug_releases": "10"},
		},
		{
			OldMeta: map[string]string{"gc.max_inactive_slug_releases": "20"},
			NewMeta: map[string]string{"gc.max_inactive_slug_releases": "20"},
		},
		{
			OldMeta: map[string]string{"foo": "bar"},
			NewMeta: map[string]string{"foo": "bar", "gc.max_inactive_slug_releases": "10"},
		},
		{
			OldMeta: map[string]string{"foo": "bar", "gc.max_inactive_slug_releases": "20"},
			NewMeta: map[string]string{"foo": "bar", "gc.max_inactive_slug_releases": "20"},
		},
	}
	for _, app := range apps {
		app.ID = random.UUID()
		c.Assert(db.Exec(`INSERT INTO apps (app_id, name, meta) VALUES ($1, $2, $3)`, app.ID, random.String(16), app.OldMeta), IsNil)
	}

	// migrate to 24, check meta was updated correctly
	m.migrateTo(24)
	for _, app := range apps {
		var meta map[string]string
		c.Assert(db.QueryRow("SELECT meta FROM apps WHERE app_id = $1", app.ID).Scan(&meta), IsNil)
		c.Assert(meta, DeepEquals, app.NewMeta)
	}
}

func (MigrateSuite) TestMigrateProcessData(c *C) {
	db := setupTestDB(c, "controllertest_migrate_process_data")
	m := &testMigrator{c: c, db: db}

	// start from ID 24
	m.migrateTo(24)

	// create some process types with data
	type oldProcType struct {
		Data bool `json:"data,omitempty"`
	}
	releases := map[string]*oldProcType{
		random.UUID(): {Data: false},
		random.UUID(): {Data: true},
	}
	for id, proc := range releases {
		procs := map[string]*oldProcType{"web": proc, "app": proc}
		c.Assert(db.Exec(`INSERT INTO releases (release_id, processes) VALUES ($1, $2)`, id, procs), IsNil)
	}

	// migrate to 25 and check Data is false and Volumes was populated correctly
	m.migrateTo(25)
	type volume struct {
		Path string `json:"path"`
	}
	type newProcType struct {
		Data    bool     `json:"data,omitempty"`
		Volumes []volume `json:"volumes,omitempty"`
	}
	for id, oldProc := range releases {
		var procs map[string]*newProcType
		c.Assert(db.QueryRow(`SELECT processes FROM releases WHERE release_id = $1`, id).Scan(&procs), IsNil)
		for _, typ := range []string{"web", "app"} {
			c.Assert(procs[typ].Data, Equals, false)
			if oldProc.Data {
				c.Assert(procs[typ].Volumes, DeepEquals, []volume{{Path: "/data"}})
			} else {
				c.Assert(procs[typ].Volumes, IsNil)
			}
		}
	}
}

func (MigrateSuite) TestMigrateReleaseAppID(c *C) {
	db := setupTestDB(c, "controllertest_migrate_release_app_id")
	m := &testMigrator{c: c, db: db}

	// start from ID 26
	m.migrateTo(26)

	// create some apps, releases and formations
	apps := []string{random.UUID(), random.UUID(), random.UUID(), random.UUID()}
	for _, app := range apps {
		c.Assert(db.Exec(`INSERT INTO apps (app_id, name) VALUES ($1, $2)`, app, app), IsNil)
	}
	releases := []string{random.UUID(), random.UUID(), random.UUID(), random.UUID()}
	for _, release := range releases {
		c.Assert(db.Exec(`INSERT INTO releases (release_id) VALUES ($1)`, release), IsNil)
	}
	// associate via formation:
	// release0 with app0
	// release1 with app1 and app2
	// release2 with no apps
	for release, app := range map[string]string{
		releases[0]: apps[0],
		releases[1]: apps[1],
		releases[1]: apps[2],
	} {
		c.Assert(db.Exec(`INSERT INTO formations (app_id, release_id) VALUES ($1, $2)`, app, release), IsNil)
	}

	// set app3 release to release3
	c.Assert(db.Exec(`UPDATE apps SET release_id = $1 WHERE app_id = $2`, releases[3], apps[3]), IsNil)

	// migrate to 27 and check the app_id column was set correctly
	m.migrateTo(27)
	for release, app := range map[string]*string{
		releases[0]: &apps[0],
		releases[1]: &apps[2],
		releases[2]: nil,
		releases[3]: &apps[3],
	} {
		var appID *string
		var deletedAt *time.Time
		c.Assert(db.QueryRow(`SELECT app_id, deleted_at FROM releases WHERE release_id = $1`, release).Scan(&appID, &deletedAt), IsNil)
		c.Assert(appID, DeepEquals, app)
		if app == nil {
			c.Assert(deletedAt, NotNil)
		} else {
			c.Assert(deletedAt, IsNil)
		}
	}
}

func (MigrateSuite) TestMigrateTLSObject(c *C) {
	db := setupTestDB(c, "controllertest_tls_object_migration")
	m := &testMigrator{c: c, db: db}

	// start from ID 39
	m.migrateTo(39)

	nRoutes := 5
	routes := make([]*router.Route, nRoutes)
	certs := make([]*tlscert.Cert, nRoutes)
	for i := 0; i < len(routes)-2; i++ {
		cert := testutils.TLSConfigForDomain(fmt.Sprintf("migrationtest%d.example.org", i))
		r := &router.Route{
			ParentRef:     fmt.Sprintf("some/parent/ref/%d", i),
			Service:       fmt.Sprintf("migrationtest%d.example.org", i),
			Domain:        fmt.Sprintf("migrationtest%d.example.org", i),
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
		certs[i] = cert
	}

	{
		// Add route with leading and trailing whitespace on cert and key
		i := len(routes) - 2
		cert := certs[i-1] // use the same cert as the previous route
		r := &router.Route{
			ParentRef:     fmt.Sprintf("some/parent/ref/%d", i),
			Service:       fmt.Sprintf("migrationtest%d.example.org", i),
			Domain:        fmt.Sprintf("migrationtest%d.example.org", i),
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
		certs[i] = cert
	}

	{
		// Add route without cert
		i := len(routes) - 1
		r := &router.Route{
			ParentRef: fmt.Sprintf("some/parent/ref/%d", i),
			Service:   fmt.Sprintf("migrationtest%d.example.org", i),
			Domain:    fmt.Sprintf("migrationtest%d.example.org", i),
		}
		err := db.QueryRow(`
			INSERT INTO http_routes (parent_ref, service, domain)
			VALUES ($1, $2, $3) RETURNING id`,
			r.ParentRef,
			r.Service,
			r.Domain).Scan(&r.ID)
		c.Assert(err, IsNil)
		routes[i] = r
	}

	for i, cert := range certs {
		if i == 0 || i >= len(certs)-2 {
			continue
		}
		c.Assert(cert.Cert, Not(Equals), certs[i-1].Cert)
	}

	// run TLS object migration
	m.migrateTo(40)

	for i, r := range routes {
		cert := certs[i]
		fetchedRoute := &router.Route{}
		var fetchedCert *string
		var fetchedCertKey *string
		var fetchedCertSHA256 *string
		err := db.QueryRow(`
			SELECT r.parent_ref, r.service, r.domain, c.cert, c.key, encode(c.cert_sha256, 'hex') FROM http_routes AS r
			LEFT OUTER JOIN route_certificates AS rc ON rc.http_route_id = r.id
			LEFT OUTER JOIN certificates AS c ON rc.certificate_id = c.id
			WHERE r.id = $1
		`, r.ID).Scan(&fetchedRoute.ParentRef, &fetchedRoute.Service, &fetchedRoute.Domain, &fetchedCert, &fetchedCertKey, &fetchedCertSHA256)
		c.Assert(err, IsNil)

		c.Assert(fetchedRoute.ParentRef, Equals, r.ParentRef)
		c.Assert(fetchedRoute.Service, Equals, r.Service)
		c.Assert(fetchedRoute.Domain, Equals, r.Domain)

		if cert == nil {
			// the last route doesn't have a cert
			c.Assert(fetchedCert, IsNil)
			c.Assert(fetchedCertKey, IsNil)
			c.Assert(fetchedCertSHA256, IsNil)
		} else {
			sum := sha256.Sum256([]byte(strings.TrimSpace(cert.Cert)))
			certSHA256 := hex.EncodeToString(sum[:])
			c.Assert(fetchedCert, Not(IsNil))
			c.Assert(fetchedCertKey, Not(IsNil))
			c.Assert(fetchedCertSHA256, Not(IsNil))
			c.Assert(strings.TrimSpace(*fetchedCert), Equals, strings.TrimSpace(cert.Cert))
			c.Assert(strings.TrimSpace(*fetchedCertKey), Equals, strings.TrimSpace(cert.PrivateKey))
			c.Assert(*fetchedCertSHA256, Equals, certSHA256)
		}
	}

	var count int64
	err := db.QueryRow(`SELECT COUNT(*) FROM certificates`).Scan(&count)
	c.Assert(err, IsNil)
	// the last two certs are the same and there's one nil after them
	c.Assert(count, Equals, int64(len(certs)-2))

	err = db.QueryRow(`SELECT COUNT(*) FROM http_routes`).Scan(&count)
	c.Assert(err, IsNil)
	c.Assert(count, Equals, int64(nRoutes))

	err = db.QueryRow(`SELECT COUNT(*) FROM route_certificates`).Scan(&count)
	c.Assert(err, IsNil)
	c.Assert(count, Equals, int64(nRoutes-1)) // the last route doesn't have a cert
}

func (MigrateSuite) TestDrainBackendsCheck(c *C) {
	db := setupTestDB(c, "controllertest_drain_backends_check_migration")
	m := &testMigrator{c: c, db: db}

	m.migrateTo(43)
	for _, v := range []bool{true, false} {
		r := &router.Route{
			ParentRef:     fmt.Sprintf("some/parent/ref/%v", v),
			Service:       "testservice",
			Domain:        fmt.Sprintf("migrationtest-%v.example.org", v),
			DrainBackends: v,
		}
		err := db.QueryRow(`
			INSERT INTO http_routes (parent_ref, service, domain, drain_backends)
			VALUES ($1, $2, $3, $4) RETURNING id`,
			r.ParentRef,
			r.Service,
			r.Domain,
			r.DrainBackends).Scan(&r.ID)
		c.Assert(err, IsNil)
	}

	m.migrateTo(44)

	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM http_routes WHERE drain_backends = false`).Scan(&count)
	c.Assert(err, IsNil)
	c.Assert(count, Equals, 0)
	err = db.QueryRow(`SELECT COUNT(*) FROM http_routes WHERE drain_backends = true`).Scan(&count)
	c.Assert(err, IsNil)
	c.Assert(count, Equals, 2)

	// try creating a new route with drain_backends false when an existing
	// route has it set to true for the same service
	r := &router.Route{
		ParentRef:     "some/parent/ref/asdf",
		Service:       "testservice",
		Domain:        "migrationtest.example.org",
		DrainBackends: false,
	}
	err = db.QueryRow(`
			INSERT INTO http_routes (parent_ref, service, domain, drain_backends)
			VALUES ($1, $2, $3, $4) RETURNING id`,
		r.ParentRef,
		r.Service,
		r.Domain,
		r.DrainBackends).Scan(&r.ID)
	c.Assert(err, Not(IsNil))
	c.Assert(err.Error(), Matches, ".*cannot create route with drain_backends.*")

	// try creating a new route with drain_backends true when an existing
	// route has it set to false for the same service
	r = &router.Route{
		ParentRef:     "some/parent/ref/asdf",
		Service:       "testservice-nodrain",
		Domain:        "migrationtest-nodrain.example.org",
		DrainBackends: false,
	}
	err = db.QueryRow(`
			INSERT INTO http_routes (parent_ref, service, domain, drain_backends)
			VALUES ($1, $2, $3, $4) RETURNING id`,
		r.ParentRef,
		r.Service,
		r.Domain,
		r.DrainBackends).Scan(&r.ID)
	c.Assert(err, IsNil)
	r = &router.Route{
		ParentRef:     "some/parent/ref/asdf",
		Service:       "testservice-nodrain",
		Domain:        "migrationtest-nodrain2.example.org",
		DrainBackends: true,
	}
	err = db.QueryRow(`
			INSERT INTO http_routes (parent_ref, service, domain, drain_backends)
			VALUES ($1, $2, $3, $4) RETURNING id`,
		r.ParentRef,
		r.Service,
		r.Domain,
		r.DrainBackends).Scan(&r.ID)
	c.Assert(err, Not(IsNil))
	c.Assert(err.Error(), Matches, ".*cannot create route with drain_backends.*")
}

func (MigrateSuite) TestMigrateDeploymentType(c *C) {
	db := setupTestDB(c, "controllertest_migrate_deployment_type")
	m := &testMigrator{c: c, db: db}

	// start from ID 46
	m.migrateTo(46)

	// create an app, and some releases, artifacts, and deployments
	app := &ct.App{
		ID:            random.UUID(),
		Name:          "migrate-deloyment-type-test-app",
		Meta:          map[string]string{},
		Strategy:      "all-at-once",
		DeployTimeout: ct.DefaultDeployTimeout,
	}
	c.Assert(db.Exec(`INSERT INTO apps (app_id, name, meta, strategy, deploy_timeout) VALUES ($1, $2, $3, $4, $5) RETURNING created_at, updated_at`, app.ID, app.Name, app.Meta, app.Strategy, app.DeployTimeout), IsNil)

	artifactIDs := []string{random.UUID(), random.UUID(), random.UUID()}
	for _, artifactID := range artifactIDs {
		c.Assert(db.Exec(`INSERT INTO artifacts (artifact_id, type, uri, meta, manifest, hashes, size, layer_url_template) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING created_at`, artifactID, "artifact-type", fmt.Sprintf("artifact://%s", artifactID), map[string]string{}, nil, nil, 1, nil), IsNil)
	}

	insertRelease := func(r *ct.Release) {
		r.AppID = app.ID
		c.Assert(db.Exec(`INSERT INTO releases (release_id, app_id, env, processes, meta) VALUES ($1, $2, $3, $4, $5) RETURNING created_at`, r.ID, r.AppID, r.Env, r.Processes, r.Meta), IsNil)
		for i, artifactID := range r.ArtifactIDs {
			c.Assert(db.Exec(`INSERT INTO release_artifacts (release_id, artifact_id, index) VALUES ($1, $2, $3)`, r.ID, artifactID, i), IsNil)
		}
	}

	deployments := make([]*ct.ExpandedDeployment, 0, 10)
	var oldReleaseID *string
	var oldRelease *ct.Release
	for i := 0; i < 10; i++ {
		d := &ct.ExpandedDeployment{
			ID:         random.UUID(),
			AppID:      app.ID,
			OldRelease: oldRelease,
			NewRelease: &ct.Release{
				ID: random.UUID(),
			},
			Strategy:      app.Strategy,
			Processes:     map[string]int{},
			Tags:          map[string]map[string]string{},
			DeployTimeout: app.DeployTimeout,
		}
		if i%2 == 0 { // every other release is a code release
			d.NewRelease.Env = map[string]string{"RELEASE_TYPE": "CODE"}
			if oldRelease == nil || len(oldRelease.ArtifactIDs) < 3 {
				d.NewRelease.ArtifactIDs = artifactIDs
			} else {
				d.NewRelease.ArtifactIDs = artifactIDs[1:]
			}
		} else {
			d.NewRelease.ArtifactIDs = oldRelease.ArtifactIDs
			d.NewRelease.Env = map[string]string{"RELEASE_TYPE": "CONFIG"}
		}
		insertRelease(d.NewRelease)
		c.Assert(db.Exec(`INSERT INTO deployments (deployment_id, app_id, old_release_id, new_release_id, strategy, processes, tags, deploy_timeout, finished_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now()) RETURNING created_at`, d.ID, d.AppID, oldReleaseID, d.NewRelease.ID, d.Strategy, d.Processes, d.Tags, d.DeployTimeout), IsNil)
		deployments = append(deployments, d)

		oldRelease = d.NewRelease
		oldReleaseID = &oldRelease.ID
	}

	// Run migration to add deployments.type
	m.migrateTo(47)

	for i := 0; i < 10; i++ {
		var releaseType *string
		c.Assert(db.QueryRow(`SELECT type FROM deployments WHERE deployment_id = $1`, deployments[i].ID).Scan(&releaseType), IsNil)
		c.Assert(releaseType, Not(IsNil))
		if i%2 == 0 {
			c.Assert(*releaseType, Equals, "code")
		} else {
			c.Assert(*releaseType, Equals, "config")
		}
	}
}

func (MigrateSuite) TestMigrateDeploymentID(c *C) {
	db := setupTestDB(c, "controllertest_migrate_deployment_id")
	m := &testMigrator{c: c, db: db}

	// start from ID 49
	m.migrateTo(49)

	// create an app, and some releases, artifacts, deployments, jobs, and events
	app := &ct.App{
		ID:            random.UUID(),
		Name:          "test-app-1",
		Meta:          map[string]string{},
		Strategy:      "all-at-once",
		DeployTimeout: ct.DefaultDeployTimeout,
	}
	c.Assert(db.Exec(`INSERT INTO apps (app_id, name, meta, strategy, deploy_timeout) VALUES ($1, $2, $3, $4, $5) RETURNING created_at, updated_at`, app.ID, app.Name, app.Meta, app.Strategy, app.DeployTimeout), IsNil)

	artifactIDs := []string{random.UUID(), random.UUID(), random.UUID()}
	for _, artifactID := range artifactIDs {
		c.Assert(db.Exec(`INSERT INTO artifacts (artifact_id, type, uri, meta, manifest, hashes, size, layer_url_template) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING created_at`, artifactID, "artifact-type", fmt.Sprintf("artifact://%s", artifactID), map[string]string{}, nil, nil, 1, nil), IsNil)
	}

	insertRelease := func(r *ct.Release) {
		r.AppID = app.ID
		c.Assert(db.Exec(`INSERT INTO releases (release_id, app_id, env, processes, meta) VALUES ($1, $2, $3, $4, $5) RETURNING created_at`, r.ID, r.AppID, r.Env, r.Processes, r.Meta), IsNil)
		for i, artifactID := range r.ArtifactIDs {
			c.Assert(db.Exec(`INSERT INTO release_artifacts (release_id, artifact_id, index) VALUES ($1, $2, $3)`, r.ID, artifactID, i), IsNil)
		}
	}

	insertDeploymentEvent := func(d *ct.ExpandedDeployment, status string, op ct.EventOp) {
		e := ct.DeploymentEvent{
			AppID:        d.AppID,
			DeploymentID: d.ID,
			ReleaseID:    d.NewRelease.ID,
			Status:       status,
		}
		c.Assert(db.Exec(`INSERT INTO events (app_id, object_id, object_type, data, op) VALUES ($1, $2, $3, $4, $5)`, d.AppID, d.ID, string(ct.EventTypeDeployment), e, string(op)), IsNil)
	}

	var oldReleaseID *string
	var oldRelease *ct.Release
	insertNewDeployment := func() *ct.ExpandedDeployment {
		d := &ct.ExpandedDeployment{
			ID:         random.UUID(),
			AppID:      app.ID,
			OldRelease: oldRelease,
			NewRelease: &ct.Release{
				ID:          random.UUID(),
				ArtifactIDs: artifactIDs,
			},
			Type:          ct.ReleaseTypeCode,
			Strategy:      app.Strategy,
			Processes:     map[string]int{},
			Tags:          map[string]map[string]string{},
			DeployTimeout: app.DeployTimeout,
		}

		oldRelease = d.NewRelease
		oldReleaseID = &oldRelease.ID

		insertRelease(d.NewRelease)
		c.Assert(db.Exec(`INSERT INTO deployments (deployment_id, app_id, old_release_id, new_release_id, type, strategy, processes, tags, deploy_timeout) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING created_at`, d.ID, d.AppID, oldReleaseID, d.NewRelease.ID, string(d.Type), d.Strategy, d.Processes, d.Tags, d.DeployTimeout), IsNil)
		insertDeploymentEvent(d, "pending", ct.EventOpCreate)

		return d
	}

	markDeploymentFinished := func(d *ct.ExpandedDeployment) {
		insertDeploymentEvent(d, "complete", ct.EventOpUpdate)
		c.Assert(db.Exec(`UPDATE deployments SET finished_at = now() WHERE deployment_id = $1`, d.ID), IsNil)
	}

	insertNewJob := func(appID, releaseID string) *ct.Job {
		j := &ct.Job{
			ID:        random.UUID(),
			UUID:      random.UUID(),
			HostID:    random.UUID(),
			AppID:     appID,
			ReleaseID: releaseID,
			State:     ct.JobStatePending,
		}
		c.Assert(db.Exec(`INSERT INTO job_cache (cluster_id, job_id, host_id, app_id, release_id, state) VALUES ($1, $2, $3, $4, $5, $6)`, j.ID, j.UUID, j.HostID, j.AppID, j.ReleaseID, string(j.State)), IsNil)

		c.Assert(db.Exec(`INSERT INTO events (app_id, object_id, object_type, data, op) VALUES ($1, $2, $3, $4, $5)`, j.AppID, j.UUID, string(ct.EventTypeJob), j, ct.EventOpCreate), IsNil)
		return j
	}

	insertNewScaleRequest := func(appID, releaseID string) *ct.ScaleRequest {
		newProcesses := map[string]int{}
		newTags := map[string]map[string]string{}
		sr := &ct.ScaleRequest{
			ID:           random.UUID(),
			AppID:        appID,
			ReleaseID:    releaseID,
			State:        ct.ScaleRequestStatePending,
			OldProcesses: map[string]int{},
			NewProcesses: &newProcesses,
			OldTags:      map[string]map[string]string{},
			NewTags:      &newTags,
		}
		c.Assert(db.Exec(`INSERT INTO scale_requests (scale_request_id, app_id, release_id, state, old_processes, new_processes, old_tags, new_tags) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, sr.ID, sr.AppID, sr.ReleaseID, string(sr.State), sr.OldProcesses, sr.NewProcesses, sr.OldTags, sr.NewTags), IsNil)

		c.Assert(db.Exec(`INSERT INTO events (app_id, object_id, object_type, data, op) VALUES ($1, $2, $3, $4, $5)`, sr.AppID, sr.ID, string(ct.EventTypeScaleRequest), sr, ct.EventOpCreate), IsNil)
		return sr
	}

	deployment1 := insertNewDeployment()

	// jobs 1 and 2 should be associated with deployment1
	job1 := insertNewJob(app.ID, deployment1.NewRelease.ID)
	job2 := insertNewJob(app.ID, deployment1.NewRelease.ID)

	// scales 1 and 2 should be associated with deployment1
	scale1 := insertNewScaleRequest(app.ID, deployment1.NewRelease.ID)
	scale2 := insertNewScaleRequest(app.ID, deployment1.NewRelease.ID)

	markDeploymentFinished(deployment1)

	// jobs 3 and 4 should not be associated with any deployment
	job3 := insertNewJob(app.ID, deployment1.NewRelease.ID)
	job4 := insertNewJob(app.ID, deployment1.NewRelease.ID)

	// scales 3 and 4 should not be associated with any deployment
	scale3 := insertNewScaleRequest(app.ID, deployment1.NewRelease.ID)
	scale4 := insertNewScaleRequest(app.ID, deployment1.NewRelease.ID)

	deployment2 := insertNewDeployment()

	// jobs 5 and 6 should be associated with deployment2
	job5 := insertNewJob(app.ID, deployment2.NewRelease.ID)
	job6 := insertNewJob(app.ID, deployment2.NewRelease.ID)

	// scales 5 and 6 should be associated with deployment2
	scale5 := insertNewScaleRequest(app.ID, deployment2.NewRelease.ID)
	scale6 := insertNewScaleRequest(app.ID, deployment2.NewRelease.ID)

	markDeploymentFinished(deployment2)

	// Run migration to add deployment_id to events, jobs, and scale_requests
	m.migrateTo(50)

	checkJobDeploymentID := func(jobID, expectedDeploymentID string, comment string, args ...interface{}) {
		var deploymentID *string
		c.Assert(db.QueryRow(`SELECT deployment_id FROM job_cache WHERE job_id = $1`, jobID).Scan(&deploymentID), IsNil)
		if expectedDeploymentID == "" {
			c.Assert(deploymentID, IsNil, Commentf(comment, args...))
		} else {
			c.Assert(deploymentID, Not(IsNil), Commentf(comment, args...))
			c.Assert(*deploymentID, Equals, expectedDeploymentID, Commentf(comment, args...))
		}

		deploymentID = nil
		c.Assert(db.QueryRow(`SELECT deployment_id FROM events WHERE object_id = $1 AND object_type = 'job'`, jobID).Scan(&deploymentID), IsNil)
		if expectedDeploymentID == "" {
			c.Assert(deploymentID, IsNil, Commentf(comment, args...))
		} else {
			c.Assert(deploymentID, Not(IsNil), Commentf(comment, args...))
			c.Assert(*deploymentID, Equals, expectedDeploymentID, Commentf(comment, args...))
		}
	}

	checkScaleRequestDeploymentID := func(scaleRequestID, expectedDeploymentID string, comment string, args ...interface{}) {
		var deploymentID *string
		c.Assert(db.QueryRow(`SELECT deployment_id FROM scale_requests WHERE scale_request_id = $1`, scaleRequestID).Scan(&deploymentID), IsNil)
		if expectedDeploymentID == "" {
			c.Assert(deploymentID, IsNil, Commentf(comment, args...))
		} else {
			c.Assert(deploymentID, Not(IsNil), Commentf(comment, args...))
			c.Assert(*deploymentID, Equals, expectedDeploymentID, Commentf(comment, args...))
		}

		deploymentID = nil
		c.Assert(db.QueryRow(`SELECT deployment_id FROM events WHERE object_id = $1 AND object_type = 'scale_request'`, scaleRequestID).Scan(&deploymentID), IsNil)
		if expectedDeploymentID == "" {
			c.Assert(deploymentID, IsNil, Commentf(comment, args...))
		} else {
			c.Assert(deploymentID, Not(IsNil), Commentf(comment, args...))
			c.Assert(*deploymentID, Equals, expectedDeploymentID, Commentf(comment, args...))
		}
	}

	checkJobDeploymentID(job1.UUID, deployment1.ID, "job1 = deployment1")
	checkJobDeploymentID(job2.UUID, deployment1.ID, "job2 = deployment1")
	checkJobDeploymentID(job3.UUID, "", "job3 = null")
	checkJobDeploymentID(job4.UUID, "", "job4 = null")
	checkJobDeploymentID(job5.UUID, deployment2.ID, "job5 = deployment2")
	checkJobDeploymentID(job6.UUID, deployment2.ID, "job6 = deployment2")

	checkScaleRequestDeploymentID(scale1.ID, deployment1.ID, "scale1 = deployment1")
	checkScaleRequestDeploymentID(scale2.ID, deployment1.ID, "scale2 = deployment1")
	checkScaleRequestDeploymentID(scale3.ID, "", "scale3 = null")
	checkScaleRequestDeploymentID(scale4.ID, "", "scale4 = null")
	checkScaleRequestDeploymentID(scale5.ID, deployment2.ID, "scale5 = deployment2")
	checkScaleRequestDeploymentID(scale6.ID, deployment2.ID, "scale6 = deployment2")
}
