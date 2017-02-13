package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	sc "github.com/flynn/flynn/pkg/sirenia/client"
	"github.com/flynn/flynn/pkg/sirenia/state"
	c "github.com/flynn/go-check"
)

type sireniaHookFunc func(t *c.C, r *ct.Release, d *sireniaFormation)
type sireniaExpectedStateFunc func(string, string) []expectedSireniaState

type tunableTest struct {
	name    string
	tunable sireniaTunable
}

type sireniaTunable struct {
	key          string
	defaultValue string
	newValue     string
}

type sireniaDatabase struct {
	appName         string
	serviceKey      string
	hostKey         string
	initDb          sireniaHookFunc
	assertWriteable sireniaHookFunc
}

type sireniaFormation struct {
	name        string
	db          sireniaDatabase
	sireniaJobs int
	webJobs     int
}

func (p *sireniaFormation) expectedAsyncs() int {
	return p.sireniaJobs - 2
}

type expectedSireniaState struct {
	Primary, Sync string
	Async         []string
}

func testDeployMultipleAsync(oldRelease, newRelease string) []expectedSireniaState {
	return []expectedSireniaState{
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
}

func testDeploySingleAsync(oldRelease, newRelease string) []expectedSireniaState {
	return []expectedSireniaState{
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
}

func sireniaRelease(client controller.Client, t *c.C, f *sireniaFormation) (*ct.Release, string) {
	// copy release from default app
	release, err := client.GetAppRelease(f.db.appName)
	t.Assert(err, c.IsNil)
	release.ID = ""
	release.Env[f.db.hostKey] = fmt.Sprintf("leader.%s.discoverd", f.name)
	release.Env[f.db.serviceKey] = f.name
	delete(release.Env, "SINGLETON")
	procName := release.Env["SIRENIA_PROCESS"]
	proc := release.Processes[procName]
	proc.Service = f.name
	release.Processes[procName] = proc
	return release, procName
}

func testSireniaDeploy(client controller.Client, disc *discoverd.Client, t *c.C, f *sireniaFormation, expectedFn sireniaExpectedStateFunc) {
	// create app
	app := &ct.App{Name: f.name, Strategy: "sirenia"}
	t.Assert(client.CreateApp(app), c.IsNil)
	defer client.DeleteApp(app.ID)

	// create release from original app
	release, procName := sireniaRelease(client, t, f)
	t.Assert(client.CreateRelease(app.ID, release), c.IsNil)
	t.Assert(client.SetAppRelease(app.ID, release.ID), c.IsNil)
	oldRelease := release.ID

	// connect discoverd event stream
	discEvents := make(chan *discoverd.Event)
	discService := disc.Service(f.name)
	discStream, err := discService.Watch(discEvents)
	t.Assert(err, c.IsNil)
	defer discStream.Close()

	// connect controller job event stream
	jobEvents := make(chan *ct.Job)
	jobStream, err := client.StreamJobEvents(f.name, jobEvents)
	t.Assert(err, c.IsNil)
	defer jobStream.Close()

	// create formation
	t.Assert(client.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{procName: f.sireniaJobs, "web": f.webJobs},
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
			debugf(t, "got cluster state: index=%d primary=%s sync=%s async=%s",
				event.ServiceMeta.Index, primary, sync, strings.Join(async, ","))
			stateCh <- stateChange{state: &state}
		}
	}()

	// wait for correct cluster state and number of web processes
	var sireniaState state.State
	var webJobs int
	ready := func() bool {
		if webJobs != f.webJobs {
			return false
		}
		if sireniaState.Primary == nil {
			return false
		}
		if f.sireniaJobs > 1 && sireniaState.Sync == nil {
			return false
		}
		if f.sireniaJobs > 2 && len(sireniaState.Async) != f.sireniaJobs-2 {
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
			sireniaState = *s.state
		case e, ok := <-jobEvents:
			if !ok {
				t.Fatalf("job event stream closed: %s", jobStream.Err())
			}
			debugf(t, "got job event: %s %s %s", e.Type, e.ID, e.State)
			if e.Type == "web" && e.State == ct.JobStateUp {
				webJobs++
			}
		case <-time.After(30 * time.Second):
			t.Fatal("timed out waiting for formation")
		}
	}

	// wait for the primary to indicate downstream replication sync
	debug(t, "waiting for primary to indicate downstream replication sync")
	sireniaClient := sc.NewClient(sireniaState.Primary.Addr)
	t.Assert(sireniaClient.WaitForReplSync(sireniaState.Sync, 1*time.Minute), c.IsNil)

	// connect to the db and run any initialisation required to later test writes
	debug(t, "initialising db")
	if f.db.initDb != nil {
		f.db.initDb(t, release, f)
	}

	// check currently writeable
	f.db.assertWriteable(t, release, f)

	// check a deploy completes with expected cluster state changes
	release.ID = ""
	t.Assert(client.CreateRelease(app.ID, release), c.IsNil)
	newRelease := release.ID
	deployment, err := client.CreateDeployment(app.ID, newRelease)
	t.Assert(err, c.IsNil)
	deployEvents := make(chan *ct.DeploymentEvent)
	deployStream, err := client.StreamDeployment(deployment, deployEvents)
	t.Assert(err, c.IsNil)
	defer deployStream.Close()

	// assertNextState checks that the next state received is in the remaining states
	// that were expected, so handles the fact that some states don't happen, but the
	// states that do happen are expected and in-order.
	assertNextState := func(remaining []expectedSireniaState) int {
		var state state.State
	loop:
		for {
			select {
			case s := <-stateCh:
				t.Assert(s.err, c.IsNil)
				if len(s.state.Async) < f.expectedAsyncs() {
					// we shouldn't usually receive states with less asyncs than
					// expected, but they can occur as an intermediate state between
					// two expected states (e.g. when a sync does a takeover at the
					// same time as a new async is started) so just ignore them.
					debug(t, "ignoring state with too few asyncs")
					continue
				}
				state = *s.state
				break loop
			case <-time.After(60 * time.Second):
				t.Fatal("timed out waiting for cluster state")
			}
		}
		if state.Primary == nil {
			t.Fatal("no primary configured")
		}
		logf := func(format string, v ...interface{}) {
			debugf(t, "skipping expected state: %s", fmt.Sprintf(format, v...))
		}
	outer:
		for i, expected := range remaining {
			if state.Primary.Meta["FLYNN_RELEASE_ID"] != expected.Primary {
				logf("primary has incorrect release")
				continue
			}
			if state.Sync == nil {
				if expected.Sync == "" {
					return i
				}
				logf("state has no sync node")
				continue
			}
			if state.Sync.Meta["FLYNN_RELEASE_ID"] != expected.Sync {
				logf("sync has incorrect release")
				continue
			}
			if state.Async == nil {
				if expected.Async == nil {
					return i
				}
				logf("state has no async nodes")
				continue
			}
			if len(state.Async) != len(expected.Async) {
				logf("expected %d asyncs, got %d", len(expected.Async), len(state.Async))
				continue
			}
			for i, release := range expected.Async {
				if state.Async[i].Meta["FLYNN_RELEASE_ID"] != release {
					logf("async[%d] has incorrect release", i)
					continue outer
				}
			}
			return i
		}
		t.Fatal("unexpected state")
		return -1
	}
	expected := expectedFn(oldRelease, newRelease)
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
			if e.JobState != ct.JobStateUp && e.JobState != ct.JobStateDown {
				continue
			}
			if e.JobType == procName {
				// move on if we have seen all the expected events
				if expectedIndex >= len(expected) {
					continue
				}
				skipped := assertNextState(expected[expectedIndex:])
				expectedIndex += 1 + skipped
			}
		case e, ok := <-jobEvents:
			if !ok {
				t.Fatalf("unexpected close of job event stream: %s", jobStream.Err())
			}
			debugf(t, "got job event: %s %s %s", e.Type, e.ID, e.State)
			if e.Type == "web" && e.State == ct.JobStateUp && e.ReleaseID == newRelease {
				newWebJobs++
			}
		}
	}

	// check we have the correct number of new web jobs
	t.Assert(newWebJobs, c.Equals, f.webJobs)

	// check writeable now deploy is complete
	f.db.assertWriteable(t, release, f)
}

func testSireniaTunables(client controller.Client, disc *discoverd.Client, t *c.C, f *sireniaFormation, tts []tunableTest) {
	// create app
	app := &ct.App{Name: f.name, Strategy: "sirenia"}
	t.Assert(client.CreateApp(app), c.IsNil)
	defer client.DeleteApp(app.ID)

	// create release from original app
	release, procName := sireniaRelease(client, t, f)
	t.Assert(client.CreateRelease(app.ID, release), c.IsNil)
	t.Assert(client.SetAppRelease(app.ID, release.ID), c.IsNil)

	// connect discoverd event stream
	discEvents := make(chan *discoverd.Event)
	discService := disc.Service(f.name)
	discStream, err := discService.Watch(discEvents)
	t.Assert(err, c.IsNil)
	defer discStream.Close()

	// connect controller job event stream
	jobEvents := make(chan *ct.Job)
	jobStream, err := client.StreamJobEvents(f.name, jobEvents)
	t.Assert(err, c.IsNil)
	defer jobStream.Close()

	// create formation
	t.Assert(client.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{procName: f.sireniaJobs, "web": f.webJobs},
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
			debugf(t, "got cluster state: index=%d primary=%s sync=%s async=%s",
				event.ServiceMeta.Index, primary, sync, strings.Join(async, ","))
			stateCh <- stateChange{state: &state}
		}
	}()

	// wait for correct cluster state and number of web processes
	var sireniaState state.State
	var webJobs int
	ready := func() bool {
		if webJobs != f.webJobs {
			return false
		}
		if sireniaState.Primary == nil {
			return false
		}
		if f.sireniaJobs > 1 && sireniaState.Sync == nil {
			return false
		}
		if f.sireniaJobs > 2 && len(sireniaState.Async) != f.sireniaJobs-2 {
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
			sireniaState = *s.state
		case e, ok := <-jobEvents:
			if !ok {
				t.Fatalf("job event stream closed: %s", jobStream.Err())
			}
			debugf(t, "got job event: %s %s %s", e.Type, e.ID, e.State)
			if e.Type == "web" && e.State == ct.JobStateUp {
				webJobs++
			}
		case <-time.After(30 * time.Second):
			t.Fatal("timed out waiting for formation")
		}
	}

	// wait for the primary to indicate downstream replication sync
	debug(t, "waiting for primary to indicate downstream replication sync")
	sireniaClient := sc.NewClient(sireniaState.Primary.Addr)
	t.Assert(sireniaClient.WaitForReplSync(sireniaState.Sync, 1*time.Minute), c.IsNil)

	// connect to the db and run any initialisation required to later test writes
	debug(t, "initialising db")
	if f.db.initDb != nil {
		f.db.initDb(t, release, f)
	}

	// check currently writeable
	f.db.assertWriteable(t, release, f)

	// run each tunable test, waiting for the database to reflect the changes on all nodes
	for _, tt := range tts {
		debug(t, "testing tunable update", tt.name, tt.tunable.key)
		tunables, err := sireniaClient.GetTunables()
		t.Assert(err, c.IsNil)

		// ensure the tunable is currently set to the default value
		curVal, ok := tunables.Data[tt.tunable.key]
		t.Assert(ok, c.Equals, true)
		t.Assert(curVal, c.Equals, tt.tunable.defaultValue)

		// issue the update command
		tunables.Data[tt.tunable.key] = tt.tunable.newValue
		tunables.Version += 1 // increment tunables version number
		t.Assert(sireniaClient.UpdateTunables(tunables), c.IsNil)

		// TODO(jpg): wait for cluster state update that contains the tunables

		instances, err := discService.Instances()
		t.Assert(err, c.IsNil)

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
	outer:
		for {
			select {
			case <-ticker.C:
				debugf(t, "checking %d instances for tunable update", len(instances))
				count := 0
				for _, inst := range instances {
					debugf(t, "checking %s tunable version", inst.Addr)
					instClient := sc.NewClient(inst.Addr)
					status, err := instClient.Status()
					t.Assert(err, c.IsNil)
					newTunables := status.Database.Config.Tunables

					// check if this node has updated to the latest tunables version yet
					if newTunables.Version != tunables.Version {
						debugf(t, "tunables version mismatch %d != %d", newTunables.Version, tunables.Version)
						break
					}
					newVal, ok := newTunables.Data[tt.tunable.key]
					t.Assert(ok, c.Equals, true)
					t.Assert(newVal, c.Equals, tt.tunable.newValue)
					count++
				}
				// stop waiting when all nodes have the new tunables applied
				if count == len(instances) {
					debugf(t, "all instances have correct tunables")
					break outer
				}
			case <-time.After(10 * time.Second):
				t.Errorf("timed out waiting for tunables")
			}
		}

		// check currently writeable
		f.db.assertWriteable(t, release, f)
	}
}
