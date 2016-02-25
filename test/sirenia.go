package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	sc "github.com/flynn/flynn/pkg/sirenia/client"
	"github.com/flynn/flynn/pkg/sirenia/state"
)

type sireniaHookFunc func(t *c.C, r *ct.Release, d *sireniaDeploy)

type sireniaDatabase struct {
	appName         string
	serviceKey      string
	hostKey         string
	initDb          sireniaHookFunc
	assertWriteable sireniaHookFunc
}

type sireniaDeploy struct {
	name        string
	db          sireniaDatabase
	sireniaJobs int
	webJobs     int
	expected    func(string, string) []expectedSireniaState
}

func (p *sireniaDeploy) expectedAsyncs() int {
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

func testSireniaDeploy(client *controller.Client, disc *discoverd.Client, t *c.C, d *sireniaDeploy) {
	// create app
	app := &ct.App{Name: d.name, Strategy: "sirenia"}
	t.Assert(client.CreateApp(app), c.IsNil)

	// copy release from default app
	release, err := client.GetAppRelease(d.db.appName)
	t.Assert(err, c.IsNil)
	release.ID = ""
	release.Env[d.db.hostKey] = fmt.Sprintf("leader.%s.discoverd", d.name)
	procName := release.Env["SIRENIA_PROCESS"]
	proc := release.Processes[procName]
	delete(proc.Env, "SINGLETON")
	proc.Env[d.db.serviceKey] = d.name
	proc.Service = d.name
	release.Processes[procName] = proc
	t.Assert(client.CreateRelease(release), c.IsNil)
	t.Assert(client.SetAppRelease(app.ID, release.ID), c.IsNil)
	oldRelease := release.ID

	// create formation
	discEvents := make(chan *discoverd.Event)
	discService := disc.Service(d.name)
	discStream, err := discService.Watch(discEvents)
	t.Assert(err, c.IsNil)
	defer discStream.Close()
	jobEvents := make(chan *ct.Job)
	jobStream, err := client.StreamJobEvents(d.name, jobEvents)
	t.Assert(err, c.IsNil)
	defer jobStream.Close()
	t.Assert(client.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{procName: d.sireniaJobs, "web": d.webJobs},
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
		if webJobs != d.webJobs {
			return false
		}
		if sireniaState.Primary == nil {
			return false
		}
		if d.sireniaJobs > 1 && sireniaState.Sync == nil {
			return false
		}
		if d.sireniaJobs > 2 && len(sireniaState.Async) != d.sireniaJobs-2 {
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

	// connect to the db so we can test writes
	d.db.initDb(t, release, d)

	// check currently writeable
	d.db.assertWriteable(t, release, d)

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
				if len(s.state.Async) < d.expectedAsyncs() {
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
		log := func(format string, v ...interface{}) {
			debugf(t, "skipping expected state: %s", fmt.Sprintf(format, v...))
		}
	outer:
		for i, expected := range remaining {
			if state.Primary.Meta["FLYNN_RELEASE_ID"] != expected.Primary {
				log("primary has incorrect release")
				continue
			}
			if state.Sync == nil {
				if expected.Sync == "" {
					return i
				}
				log("state has no sync node")
				continue
			}
			if state.Sync.Meta["FLYNN_RELEASE_ID"] != expected.Sync {
				log("sync has incorrect release")
				continue
			}
			if state.Async == nil {
				if expected.Async == nil {
					return i
				}
				log("state has no async nodes")
				continue
			}
			if len(state.Async) != len(expected.Async) {
				log("expected %d asyncs, got %d", len(expected.Async), len(state.Async))
				continue
			}
			for i, release := range expected.Async {
				if state.Async[i].Meta["FLYNN_RELEASE_ID"] != release {
					log("async[%d] has incorrect release", i)
					continue outer
				}
			}
			return i
		}
		t.Fatal("unexpected state")
		return -1
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
			if e.JobState != ct.JobStateUp && e.JobState != ct.JobStateDown {
				continue
			}
			switch e.JobType {
			case procName:
				// move on if we have seen all the expected events
				if expectedIndex >= len(expected) {
					continue
				}
				skipped := assertNextState(expected[expectedIndex:])
				expectedIndex += 1 + skipped
			case "web":
				if e.JobState == ct.JobStateUp && e.ReleaseID == newRelease {
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
	d.db.assertWriteable(t, release, d)
}
