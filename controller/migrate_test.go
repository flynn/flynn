package main

import (
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
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

// TestMigrateReleaseArtifacts checks that migrating to ID 14 correctly
// migrates releases by creating appropriate records in the release_artifacts
// table
func (MigrateSuite) TestMigrateReleaseArtifacts(c *C) {
	db := setupTestDB(c, "controllertest_migrate_release_artifacts")
	m := &testMigrator{c: c, db: db}

	// start from ID 13
	m.migrateTo(13)

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

	// insert a slug based release
	slugReleaseID := random.UUID()
	imageArtifactID := random.UUID()
	slugEnv := map[string]string{"SLUG_URL": "http://example.com/slug.tgz"}
	c.Assert(db.Exec(`INSERT INTO artifacts (artifact_id, type, uri) VALUES ($1, $2, $3)`, imageArtifactID, "docker", "http://example.com/"+imageArtifactID), IsNil)
	c.Assert(db.Exec(`INSERT INTO releases (release_id, artifact_id, env) VALUES ($1, $2, $3)`, slugReleaseID, imageArtifactID, slugEnv), IsNil)
	releaseArtifacts[slugReleaseID] = imageArtifactID

	// migrate to 14 and check release_artifacts was populated correctly
	m.migrateTo(14)
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

	// check the slug release got "git=true" in metadata
	var releaseMeta map[string]string
	err = db.QueryRow("SELECT meta FROM releases WHERE release_id = $1", slugReleaseID).Scan(&releaseMeta)
	c.Assert(err, IsNil)
	c.Assert(releaseMeta, DeepEquals, map[string]string{"git": "true"})

	// check the slug release got a file artifact with the correct URI and meta
	var slugURI string
	var artifactMeta map[string]string
	err = db.QueryRow("SELECT uri, meta FROM artifacts INNER JOIN release_artifacts USING (artifact_id) WHERE type = 'file' AND release_id = $1", slugReleaseID).Scan(&slugURI, &artifactMeta)
	c.Assert(err, IsNil)
	c.Assert(slugURI, Equals, slugEnv["SLUG_URL"])
	c.Assert(artifactMeta, DeepEquals, map[string]string{"blobstore": "true"})
}
