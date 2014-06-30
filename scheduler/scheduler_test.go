package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/flynn/flynn-controller/client"
	tu "github.com/flynn/flynn-controller/testutils"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-host/types"
	. "github.com/titanous/gocheck"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct{}

var _ = Suite(&S{})

func newFakeControllerClient(appID string, release *ct.Release, artifact *ct.Artifact, processes map[string]int, stream chan *ct.ExpandedFormation) *fakeControllerClient {
	return &fakeControllerClient{
		releases:  map[string]*ct.Release{release.ID: release},
		artifacts: map[string]*ct.Artifact{artifact.ID: artifact},
		formations: map[formationKey]*ct.Formation{
			formationKey{appID, release.ID}: {AppID: appID, ReleaseID: release.ID, Processes: processes},
		},
		jobs:   make(map[string]*ct.Job),
		stream: stream,
	}
}

type fakeControllerClient struct {
	releases   map[string]*ct.Release
	artifacts  map[string]*ct.Artifact
	formations map[formationKey]*ct.Formation
	jobs       map[string]*ct.Job
	stream     chan *ct.ExpandedFormation
}

func (c *fakeControllerClient) GetRelease(releaseID string) (*ct.Release, error) {
	if release, ok := c.releases[releaseID]; ok {
		return release, nil
	}
	return nil, controller.ErrNotFound
}

func (c *fakeControllerClient) GetArtifact(artifactID string) (*ct.Artifact, error) {
	if artifact, ok := c.artifacts[artifactID]; ok {
		return artifact, nil
	}
	return nil, controller.ErrNotFound
}

func (c *fakeControllerClient) GetFormation(appID, releaseID string) (*ct.Formation, error) {
	if formation, ok := c.formations[formationKey{appID, releaseID}]; ok {
		return formation, nil
	}
	return nil, controller.ErrNotFound
}

func (c *fakeControllerClient) StreamFormations(since *time.Time) (*controller.FormationUpdates, *error) {
	var err error
	return &controller.FormationUpdates{Chan: c.stream}, &err
}

func (c *fakeControllerClient) PutJob(job *ct.Job) error {
	c.jobs[job.ID] = job
	return nil
}

func (c *fakeControllerClient) setFormationStream(s chan *ct.ExpandedFormation) {
	c.stream = s
}

type formationUpdate struct {
	processes map[string]int
}

func (f *formationUpdate) jobCount() int {
	var count int
	for _, num := range f.processes {
		count += num
	}
	return count
}

func waitForFormationEvent(events <-chan *FormationEvent, c *C) {
	select {
	case <-events:
	case <-time.After(time.Second):
		c.Fatal("timed out waiting for Formation event")
	}
}

func waitForHostEvents(count int, events <-chan *host.Event, c *C) {
	for i := 0; i < count; i++ {
		select {
		case <-events:
		case <-time.After(time.Second):
			c.Fatal("timed out waiting for Host event")
		}
	}
}

func waitForJobStartEvent(events <-chan *host.Event, c *C) *host.Event {
	for {
		select {
		case e := <-events:
			if e.Event == "start" {
				return e
			}
		case <-time.After(time.Second):
			c.Fatal("timed out waiting for Job Start event")
			return nil
		}
	}
}

func waitForWatchHostStart(events <-chan *host.Event, c *C) {
	select {
	case e := <-events:
		if e == nil {
			return
		}
		c.Fatalf("expected sentinel event, got %+v", e)
	case <-time.After(time.Second):
		c.Fatal("timed out waiting for watchHost start event")
	}
}

func newRelease(id string, artifact *ct.Artifact, processes map[string]int) *ct.Release {
	processTypes := make(map[string]ct.ProcessType, len(processes))
	for t, _ := range processes {
		processTypes[t] = ct.ProcessType{Cmd: []string{"start", t}}
	}

	return &ct.Release{
		ID:         id,
		ArtifactID: artifact.ID,
		Processes:  processTypes,
	}
}

func newFakeCluster(hostID, appID, releaseID string, processes map[string]int, jobs []*host.Job) *tu.FakeCluster {
	if jobs == nil {
		jobs = make([]*host.Job, 0)
	}
	for t, c := range processes {
		for i := 0; i < c; i++ {
			job := &host.Job{
				ID: fmt.Sprintf("job%d", i),
				Attributes: map[string]string{
					"flynn-controller.app":     appID,
					"flynn-controller.release": releaseID,
					"flynn-controller.type":    t,
				},
			}
			jobs = append(jobs, job)
		}
	}

	cl := tu.NewFakeCluster()
	cl.SetHosts(map[string]host.Host{hostID: host.Host{ID: hostID, Jobs: jobs}})
	cl.SetHostClient(hostID, tu.NewFakeHostClient(hostID))
	return cl
}

func testAfterFunc(durations *[]time.Duration) func(d time.Duration, f func()) *time.Timer {
	return func(d time.Duration, f func()) *time.Timer {
		*durations = append(*durations, d)
		f()
		return nil
	}
}

func (s *S) TestWatchFormations(c *C) {
	// Create a fake cluster with an existing running formation
	appID := "existing-app"
	artifact := &ct.Artifact{ID: "existing-artifact"}
	processes := map[string]int{"web": 1}
	release := newRelease("existing-release", artifact, processes)
	stream := make(chan *ct.ExpandedFormation)
	cc := newFakeControllerClient(appID, release, artifact, processes, stream)

	hostID := "host0"
	cl := newFakeCluster(hostID, appID, release.ID, processes, nil)

	cx := newContext(cc, cl)
	events := make(chan *FormationEvent)
	defer close(events)
	go cx.watchFormations(events)

	// Give the scheduler chance to sync with the cluster, then check it's in sync
	waitForFormationEvent(events, c)
	c.Assert(cx.formations.Len(), Equals, 1)
	formation := cx.formations.Get(appID, release.ID)
	c.Assert(formation.AppID, Equals, appID)
	c.Assert(formation.Release, DeepEquals, release)
	c.Assert(formation.Artifact, DeepEquals, artifact)
	c.Assert(formation.Processes, DeepEquals, processes)
	c.Assert(cx.jobs.Len(), Equals, 1)
	job := cx.jobs.Get(hostID, "job0")
	c.Assert(job.Type, Equals, "web")

	f := &ct.ExpandedFormation{
		App: &ct.App{ID: "app0"},
		Release: &ct.Release{
			ID:         "release0",
			ArtifactID: "artifact0",
			Processes: map[string]ct.ProcessType{
				"web":    ct.ProcessType{Cmd: []string{"start", "web"}},
				"worker": ct.ProcessType{Cmd: []string{"start", "worker"}},
			},
		},
		Artifact:  &ct.Artifact{ID: "artifact0", Type: "docker", URI: "docker://foo/bar"},
		UpdatedAt: time.Now(),
	}

	updates := []*formationUpdate{
		&formationUpdate{processes: map[string]int{"web": 2}},
		&formationUpdate{processes: map[string]int{"web": 3, "worker": 1}},
		&formationUpdate{processes: map[string]int{"web": 1}},
	}

	for _, u := range updates {
		f.Processes = u.processes
		stream <- f
		waitForFormationEvent(events, c)

		c.Assert(cx.formations.Len(), Equals, 2)
		formation = cx.formations.Get(f.App.ID, f.Release.ID)
		c.Assert(formation.AppID, Equals, f.App.ID)
		c.Assert(formation.Release, DeepEquals, f.Release)
		c.Assert(formation.Artifact, DeepEquals, f.Artifact)
		c.Assert(formation.Processes, DeepEquals, f.Processes)

		host := cl.GetHost(hostID)
		c.Assert(len(host.Jobs), Equals, u.jobCount()+1)

		processes := make(map[string]int, len(u.processes))
		for _, job := range host.Jobs {
			jobType := job.Attributes["flynn-controller.type"]
			processes[jobType]++
		}
		// Ignore the existing web job
		processes["web"]--
		c.Assert(processes, DeepEquals, u.processes)
	}

	// check scheduler reconnects
	newStream := make(chan *ct.ExpandedFormation)
	cc.setFormationStream(newStream)
	close(stream)
	f.Processes = map[string]int{"web": 3}
	newStream <- f
	waitForFormationEvent(events, c)
	c.Assert(cx.formations.Len(), Equals, 2)
	host := cl.GetHost(hostID)
	c.Assert(len(host.Jobs), Equals, 4)
}

func (s *S) TestWatchHost(c *C) {
	// Create a fake cluster with an existing running formation and a one-off job
	appID := "app"
	artifact := &ct.Artifact{ID: "artifact", Type: "docker", URI: "docker://foo/bar"}
	processes := map[string]int{"web": 3}
	release := newRelease("release", artifact, processes)
	cc := newFakeControllerClient(appID, release, artifact, processes, nil)

	hostID := "host0"
	cl := newFakeCluster(hostID, appID, release.ID, processes, []*host.Job{
		{ID: "one-off-job", Attributes: map[string]string{"flynn-controller.app": appID, "flynn-controller.release": release.ID}},
	})

	hc := tu.NewFakeHostClient(hostID)
	cl.SetHostClient(hostID, hc)

	cx := newContext(cc, cl)
	events := make(chan *host.Event, 4)
	defer close(events)
	cx.syncCluster(events)
	c.Assert(cx.jobs.Len(), Equals, 4)
	c.Assert(len(cl.GetHost(hostID).Jobs), Equals, 4)

	// Give the watchHost goroutine chance to start
	waitForWatchHostStart(events, c)

	// Check jobs are marked as up once started
	hc.SendEvent("start", "job0")
	hc.SendEvent("start", "job1")
	hc.SendEvent("start", "job2")
	hc.SendEvent("start", "one-off-job")
	waitForHostEvents(4, events, c)
	c.Assert(len(cc.jobs), Equals, 4)
	c.Assert(cc.jobs[hostID+"-job0"].State, Equals, "up")
	c.Assert(cc.jobs[hostID+"-job1"].State, Equals, "up")
	c.Assert(cc.jobs[hostID+"-job2"].State, Equals, "up")
	c.Assert(cc.jobs[hostID+"-one-off-job"].State, Equals, "up")

	// Check that when a formation's job is removed, it is marked as down and a new one is scheduled
	cl.RemoveJob(hostID, "job0", false)
	waitForHostEvents(2, events, c) // wait for both a stop and start event
	c.Assert(cc.jobs[hostID+"-job0"].State, Equals, "down")
	c.Assert(cx.jobs.Len(), Equals, 4)
	c.Assert(len(cl.GetHost(hostID).Jobs), Equals, 4)
	job, _ := hc.GetJob("job0")
	c.Assert(job, IsNil)

	// Check that when a one-off job is removed, it is marked as down but a new one is not scheduled
	cl.RemoveJob(hostID, "one-off-job", false)
	waitForHostEvents(1, events, c)
	c.Assert(cc.jobs[hostID+"-one-off-job"].State, Equals, "down")
	c.Assert(cx.jobs.Len(), Equals, 3)
	c.Assert(len(cl.GetHost(hostID).Jobs), Equals, 3)
	job, _ = hc.GetJob("one-off-job")
	c.Assert(job, IsNil)

	// Check that when a job errors, it is marked as crashed and a new one is started
	cl.RemoveJob(hostID, "job1", true)
	waitForHostEvents(2, events, c) // wait for both an error and start event
	c.Assert(cc.jobs[hostID+"-job1"].State, Equals, "crashed")
	c.Assert(cx.jobs.Len(), Equals, 3)
	c.Assert(len(cl.GetHost(hostID).Jobs), Equals, 3)
	job, _ = hc.GetJob("job1")
	c.Assert(job, IsNil)
}

func (s *S) TestJobRestartBackoffPolicy(c *C) {
	// Create a fake cluster with an existing running formation
	appID := "app"
	artifact := &ct.Artifact{ID: "artifact", Type: "docker", URI: "docker://foo/bar"}
	processes := map[string]int{"web": 2}
	release := newRelease("release", artifact, processes)
	cc := newFakeControllerClient(appID, release, artifact, processes, nil)

	hostID := "host0"
	cl := newFakeCluster(hostID, appID, release.ID, processes, nil)

	hc := tu.NewFakeHostClient(hostID)
	cl.SetHostClient(hostID, hc)

	cx := newContext(cc, cl)
	events := make(chan *host.Event, 2)
	defer close(events)
	cx.syncCluster(events)
	c.Assert(cx.jobs.Len(), Equals, 2)

	// Give the watchHost goroutine chance to start
	waitForWatchHostStart(events, c)

	durations := make([]time.Duration, 0)
	timeAfterFunc = testAfterFunc(&durations)

	// First restart: scheduled immediately
	cl.RemoveJob(hostID, "job0", false)
	e := waitForJobStartEvent(events, c)
	c.Assert(len(durations), Equals, 0)

	// Second restart: scheduled for 1 * backoffPeriod
	cl.RemoveJob(hostID, e.JobID, false)
	e = waitForJobStartEvent(events, c)
	c.Assert(len(durations), Equals, 1)
	c.Assert(durations[0], Equals, backoffPeriod)

	// Third restart: scheduled for 2 * backoffPeriod
	cl.RemoveJob(hostID, e.JobID, false)
	e = waitForJobStartEvent(events, c)
	c.Assert(len(durations), Equals, 2)
	c.Assert(durations[1], Equals, 2*backoffPeriod)

	// After backoffPeriod has elapsed: scheduled immediately
	job := cx.jobs.Get(hostID, e.JobID)
	job.startedAt = time.Now().Add(-backoffPeriod - 1*time.Second)
	cl.RemoveJob(hostID, job.ID, false)
	waitForJobStartEvent(events, c)
	c.Assert(len(durations), Equals, 2)
}
