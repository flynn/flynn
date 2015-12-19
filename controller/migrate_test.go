package main

import (
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

type MigrateSuite struct{}

var _ = Suite(&MigrateSuite{})

// TestMigrateJobStates checks that migrating to ID 9 does not break existing
// job records
func (MigrateSuite) TestMigrateJobStates(c *C) {
	db := setupTestDB(c, "controller_migrate_test")

	currID := 0
	migrateTo := func(id int) {
		c.Assert((*migrations)[currID:id].Migrate(db), IsNil)
		currID = id
	}

	// start from ID 7
	migrateTo(7)

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
	migrateTo(8)
	err := db.Exec(`UPDATE job_cache SET state = 'foo' WHERE job_id = $1`, jobID)
	c.Assert(err, NotNil)
	if !postgres.IsPostgresCode(err, postgres.ForeignKeyViolation) {
		c.Fatalf("expected postgres foreign key violation, got %s", err)
	}

	// migrate to 9 and check job IDs are correct, pending state is valid
	migrateTo(9)
	var clusterID, dbUUID, dbHostID string
	c.Assert(db.QueryRow("SELECT cluster_id, job_id, host_id FROM job_cache WHERE cluster_id = $1", jobID).Scan(&clusterID, &dbUUID, &dbHostID), IsNil)
	c.Assert(clusterID, Equals, jobID)
	c.Assert(dbUUID, Equals, uuid)
	c.Assert(dbHostID, Equals, hostID)
	c.Assert(db.Exec(`UPDATE job_cache SET state = 'pending' WHERE job_id = $1`, uuid), IsNil)
}
