package main

import (
	"time"

	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"

	. "github.com/flynn/go-check"
)

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
	apps := []string{random.UUID(), random.UUID(), random.UUID()}
	for _, app := range apps {
		c.Assert(db.Exec(`INSERT INTO apps (app_id, name) VALUES ($1, $2)`, app, app), IsNil)
	}
	releases := []string{random.UUID(), random.UUID(), random.UUID()}
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

	// migrate to 27 and check the app_id column was set correctly
	m.migrateTo(27)
	for release, app := range map[string]*string{
		releases[0]: &apps[0],
		releases[1]: &apps[2],
		releases[2]: nil,
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
