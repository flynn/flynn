package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/appliance/postgresql/state"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/postgres"
)

type PostgresSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&PostgresSuite{})

// Check postgres config to avoid regressing on https://github.com/flynn/flynn/issues/101
func (s *PostgresSuite) TestSSLRenegotiationLimit(t *c.C) {
	query := flynn(t, "/", "-a", "controller", "pg", "psql", "--", "-c", "SHOW ssl_renegotiation_limit")
	t.Assert(query, SuccessfulOutputContains, "ssl_renegotiation_limit \n-------------------------\n 0\n(1 row)")
}

func (s *PostgresSuite) TestDumpRestore(t *c.C) {
	r := s.newGitRepo(t, "empty")
	t.Assert(r.flynn("create"), Succeeds)

	t.Assert(r.flynn("resource", "add", "postgres"), Succeeds)

	t.Assert(r.flynn("pg", "psql", "--", "-c",
		"CREATE table foos (data text); INSERT INTO foos (data) VALUES ('foobar')"), Succeeds)

	file := filepath.Join(t.MkDir(), "db.dump")
	t.Assert(r.flynn("pg", "dump", "-f", file), Succeeds)
	t.Assert(r.flynn("pg", "psql", "--", "-c", "DROP TABLE foos"), Succeeds)

	r.flynn("pg", "restore", "-f", file)

	query := r.flynn("pg", "psql", "--", "-c", "SELECT * FROM foos")
	t.Assert(query, SuccessfulOutputContains, "foobar")
}

type pgDeploy struct {
	name     string
	pgJobs   int
	webJobs  int
	expected func(string, string) []expectedPgState
}

type expectedPgState struct {
	Primary, Sync string
	Async         []string
}

func (s *PostgresSuite) TestDeployMultipleAsync(t *c.C) {
	s.testDeploy(t, &pgDeploy{
		name:    "postgres-multiple-async",
		pgJobs:  5,
		webJobs: 2,
		expected: func(oldRelease, newRelease string) []expectedPgState {
			return []expectedPgState{
				// new Async[3], kill Async[0]
				{Primary: oldRelease, Sync: oldRelease, Async: []string{oldRelease, oldRelease, oldRelease, newRelease}},
				{Primary: oldRelease, Sync: oldRelease, Async: []string{oldRelease, oldRelease, newRelease}},

				// new Async[3], kill Async[0]
				{Primary: oldRelease, Sync: oldRelease, Async: []string{oldRelease, oldRelease, newRelease, newRelease}},
				{Primary: oldRelease, Sync: oldRelease, Async: []string{oldRelease, newRelease, newRelease}},

				// new Async[3], kill Async[0]
				{Primary: oldRelease, Sync: oldRelease, Async: []string{oldRelease, newRelease, newRelease, newRelease}},
				{Primary: oldRelease, Sync: oldRelease, Async: []string{newRelease, newRelease, newRelease}},

				// new Async[3], kill Sync
				{Primary: oldRelease, Sync: oldRelease, Async: []string{newRelease, newRelease, newRelease, newRelease}},
				{Primary: oldRelease, Sync: newRelease, Async: []string{newRelease, newRelease, newRelease}},

				// new Async[3], kill Primary
				{Primary: oldRelease, Sync: newRelease, Async: []string{newRelease, newRelease, newRelease, newRelease}},
				{Primary: newRelease, Sync: newRelease, Async: []string{newRelease, newRelease, newRelease}},
			}
		},
	})
}

func (s *PostgresSuite) TestDeploySingleAsync(t *c.C) {
	s.testDeploy(t, &pgDeploy{
		name:    "postgres-single-async",
		pgJobs:  3,
		webJobs: 2,
		expected: func(oldRelease, newRelease string) []expectedPgState {
			return []expectedPgState{
				// new Async[1], kill Async[0]
				{Primary: oldRelease, Sync: oldRelease, Async: []string{oldRelease, newRelease}},
				{Primary: oldRelease, Sync: oldRelease, Async: []string{newRelease}},

				// new Async[1], kill Sync
				{Primary: oldRelease, Sync: oldRelease, Async: []string{newRelease, newRelease}},
				{Primary: oldRelease, Sync: newRelease, Async: []string{newRelease}},

				// new Async[1], kill Primary
				{Primary: oldRelease, Sync: newRelease, Async: []string{newRelease, newRelease}},
				{Primary: newRelease, Sync: newRelease, Async: []string{newRelease}},
			}
		},
	})
}

func (s *PostgresSuite) testDeploy(t *c.C, d *pgDeploy) {
	// create postgres app
	client := s.controllerClient(t)
	app := &ct.App{Name: d.name, Strategy: "postgres"}
	t.Assert(client.CreateApp(app), c.IsNil)

	// copy release from default postgres app
	release, err := client.GetAppRelease("postgres")
	t.Assert(err, c.IsNil)
	release.ID = ""
	proc := release.Processes["postgres"]
	delete(proc.Env, "SINGLETON")
	proc.Env["FLYNN_POSTGRES"] = d.name
	proc.Service = d.name
	release.Processes["postgres"] = proc
	t.Assert(client.CreateRelease(release), c.IsNil)
	t.Assert(client.SetAppRelease(app.ID, release.ID), c.IsNil)
	oldRelease := release.ID

	// create formation
	discEvents := make(chan *discoverd.Event)
	discStream, err := s.discoverdClient(t).Service(d.name).Watch(discEvents)
	t.Assert(err, c.IsNil)
	defer discStream.Close()
	jobEvents := make(chan *ct.JobEvent)
	jobStream, err := client.StreamJobEvents(d.name, jobEvents)
	t.Assert(err, c.IsNil)
	defer jobStream.Close()
	t.Assert(client.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"postgres": d.pgJobs, "web": d.webJobs},
	}), c.IsNil)

	// watch cluster state changes
	type stateChange struct {
		state *state.State
		err   error
	}
	stateCh := make(chan stateChange)
	go func() {
		for event := range discEvents {
			if event.Kind != discoverd.EventKindServiceMeta {
				continue
			}
			var state state.State
			if err := json.Unmarshal(event.ServiceMeta.Data, &state); err != nil {
				stateCh <- stateChange{err: err}
				return
			}
			primary := ""
			if state.Primary != nil {
				primary = state.Primary.Addr
			}
			sync := ""
			if state.Sync != nil {
				sync = state.Sync.Addr
			}
			var async []string
			for _, a := range state.Async {
				async = append(async, a.Addr)
			}
			debugf(t, "got pg cluster state: index=%d primary=%s sync=%s async=%s",
				event.ServiceMeta.Index, primary, sync, strings.Join(async, ","))
			stateCh <- stateChange{state: &state}
		}
	}()

	// wait for correct cluster state and number of web processes
	var pgState state.State
	var webJobs int
	ready := func() bool {
		if webJobs != d.webJobs {
			return false
		}
		if pgState.Primary == nil {
			return false
		}
		if d.pgJobs > 1 && pgState.Sync == nil {
			return false
		}
		if d.pgJobs > 2 && len(pgState.Async) != d.pgJobs-2 {
			return false
		}
		return true
	}
	for {
		if ready() {
			break
		}
		select {
		case s := <-stateCh:
			t.Assert(s.err, c.IsNil)
			pgState = *s.state
		case e, ok := <-jobEvents:
			if !ok {
				t.Fatalf("job event stream closed: %s", jobStream.Err())
			}
			debugf(t, "got job event: %s %s %s", e.Type, e.JobID, e.State)
			if e.Type == "web" && e.State == "up" {
				webJobs++
			}
		case <-time.After(30 * time.Second):
			t.Fatal("timed out waiting for postgres formation")
		}
	}

	// connect to the db so we can test writes
	db := postgres.Wait(d.name, fmt.Sprintf("dbname=postgres user=flynn password=%s", release.Env["PGPASSWORD"]))
	dbname := "deploy-test"
	t.Assert(db.Exec(fmt.Sprintf(`CREATE DATABASE "%s" WITH OWNER = "flynn"`, dbname)), c.IsNil)
	db.Close()
	db, err = postgres.Open(d.name, fmt.Sprintf("dbname=%s user=flynn password=%s", dbname, release.Env["PGPASSWORD"]))
	t.Assert(err, c.IsNil)
	defer db.Close()
	t.Assert(db.Exec(`CREATE TABLE deploy_test ( data text)`), c.IsNil)
	assertWriteable := func() {
		debug(t, "writing to postgres database")
		t.Assert(db.Exec(`INSERT INTO deploy_test (data) VALUES ('data')`), c.IsNil)
	}

	// check currently writeable
	assertWriteable()

	// check a deploy completes with expected cluster state changes
	release.ID = ""
	t.Assert(client.CreateRelease(release), c.IsNil)
	newRelease := release.ID
	deployment, err := client.CreateDeployment(app.ID, newRelease)
	t.Assert(err, c.IsNil)
	deployEvents := make(chan *ct.DeploymentEvent)
	deployStream, err := client.StreamDeployment(deployment, deployEvents)
	t.Assert(err, c.IsNil)
	defer deployStream.Close()

	assertNextState := func(expected expectedPgState) {
		var state state.State
		select {
		case s := <-stateCh:
			t.Assert(s.err, c.IsNil)
			state = *s.state
		case <-time.After(60 * time.Second):
			t.Fatal("timed out waiting for postgres cluster state")
		}
		if state.Primary == nil {
			t.Fatal("no primary configured")
		}
		if state.Primary.Meta["FLYNN_RELEASE_ID"] != expected.Primary {
			t.Fatal("primary has incorrect release")
		}
		if expected.Sync == "" {
			return
		}
		if state.Sync == nil {
			t.Fatal("no sync configured")
		}
		if state.Sync.Meta["FLYNN_RELEASE_ID"] != expected.Sync {
			t.Fatal("sync has incorrect release")
		}
		if expected.Async == nil {
			return
		}
		if len(state.Async) != len(expected.Async) {
			t.Fatalf("expected %d asyncs, got %d", len(expected.Async), len(state.Async))
		}
		for i, release := range expected.Async {
			if state.Async[i].Meta["FLYNN_RELEASE_ID"] != release {
				t.Fatalf("async[%d] has incorrect release", i)
			}
		}
	}
	expected := d.expected(oldRelease, newRelease)
	var expectedIndex, newWebJobs int
loop:
	for {
		select {
		case e, ok := <-deployEvents:
			if !ok {
				t.Fatal("unexpected close of deployment event stream")
			}
			switch e.Status {
			case "complete":
				break loop
			case "failed":
				t.Fatalf("deployment failed: %s", e.Error)
			}
			debugf(t, "got deployment event: %s %s", e.JobType, e.JobState)
			if e.JobState != "up" && e.JobState != "down" {
				continue
			}
			switch e.JobType {
			case "postgres":
				assertNextState(expected[expectedIndex])
				expectedIndex++
			case "web":
				if e.JobState == "up" && e.ReleaseID == newRelease {
					newWebJobs++
				}
			}
		case <-time.After(2 * time.Minute):
			t.Fatal("timed out waiting for deployment")
		}
	}

	// check we have the correct number of new web jobs
	t.Assert(newWebJobs, c.Equals, d.webJobs)

	// check writeable now deploy is complete
	assertWriteable()
}
