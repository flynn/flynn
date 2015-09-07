package main

import (
	"fmt"
	"testing"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	. "github.com/flynn/flynn/controller/testutils"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/stream"
)

func Test(t *testing.T) { TestingT(t) }

type TestSuite struct{}

var _ = Suite(&TestSuite{})

const (
	testAppID      = "app-1"
	testHostID     = "host-1"
	testArtifactId = "artifact-1"
	testReleaseID  = "release-1"
	testJobType    = "web"
	testJobCount   = 1
)

func createTestScheduler(cluster utils.ClusterClient) *Scheduler {
	app := &ct.App{ID: testAppID, Name: testAppID}
	artifact := &ct.Artifact{ID: testArtifactId}
	processes := map[string]int{testJobType: testJobCount}
	release := NewRelease(testReleaseID, artifact, processes)
	cc := NewFakeControllerClient()
	cc.CreateApp(app)
	cc.CreateArtifact(artifact)
	cc.CreateRelease(release)
	cc.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	s := NewScheduler(cluster, cc)

	return s
}

func runTestScheduler(cluster utils.ClusterClient, events chan Event, isLeader bool) *TestScheduler {
	s := createTestScheduler(cluster)

	stream := s.Subscribe(events)
	go s.Run()
	s.ChangeLeader(isLeader)

	return &TestScheduler{
		scheduler: s,
		stream:    stream,
	}
}

type TestScheduler struct {
	scheduler *Scheduler
	stream    stream.Stream
}

func (s *TestScheduler) Stop() {
	s.scheduler.Stop()
	s.stream.Close()
}

func waitDurationForEvent(events chan Event, typ EventType, duration time.Duration) (Event, error) {
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return nil, fmt.Errorf("unexpected close of scheduler event stream")
			}
			if event.Type() == typ {
				if err := event.Err(); err != nil {
					return event, fmt.Errorf("unexpected event error: %s", err)
				}
				return event, nil
			}
		case <-time.After(duration):
			return nil, fmt.Errorf("timed out waiting for %s event", typ)
		}
	}
}

func waitForEvent(events chan Event, typ EventType) (Event, error) {
	return waitDurationForEvent(events, typ, 2*time.Second)
}

func (ts *TestSuite) TestSingleJobStart(c *C) {
	h := NewFakeHostClient(testHostID)
	cluster := NewFakeCluster()
	cluster.SetHosts(map[string]*FakeHostClient{h.ID(): h})
	events := make(chan Event, eventBufferSize)
	sched := runTestScheduler(cluster, events, true)
	defer sched.Stop()
	s := sched.scheduler

	// wait for a rectify jobs event
	c.Log("Waiting for a rectify jobs event")
	_, err := waitForEvent(events, EventTypeRectify)
	c.Assert(err, IsNil)
	e, err := waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	event, ok := e.(*JobStartEvent)
	c.Assert(ok, Equals, true)
	c.Assert(event.Job, NotNil)
	job := event.Job
	c.Assert(job.Type, Equals, testJobType)
	c.Assert(job.AppID, Equals, testAppID)
	c.Assert(job.ReleaseID, Equals, testReleaseID)

	// Query the scheduler for the same job
	c.Log("Verify that the scheduler has the same job")
	jobs := s.Jobs()
	c.Assert(jobs, HasLen, 1)
	for _, job := range jobs {
		c.Assert(job.Type, Equals, testJobType)
		c.Assert(job.HostID, Equals, testHostID)
		c.Assert(job.AppID, Equals, testAppID)
	}
}

func (ts *TestSuite) TestFormationChange(c *C) {
	h := NewFakeHostClient(testHostID)
	cluster := NewFakeCluster()
	cluster.SetHosts(map[string]*FakeHostClient{h.ID(): h})
	events := make(chan Event, eventBufferSize)
	sched := runTestScheduler(cluster, events, true)
	defer sched.Stop()
	s := sched.scheduler

	_, err := waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)

	app, err := s.GetApp(testAppID)
	c.Assert(err, IsNil)
	release, err := s.GetRelease(testReleaseID)
	c.Assert(err, IsNil)
	artifact, err := s.GetArtifact(release.ArtifactID)
	c.Assert(err, IsNil)

	// Test scaling up an existing formation
	c.Log("Test scaling up an existing formation. Wait for formation change and job start")
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: map[string]int{"web": 4}})
	_, err = waitForEvent(events, EventTypeFormationChange)
	c.Assert(err, IsNil)
	e, err := waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	job := checkJobStartEvent(c, e)
	c.Assert(job.Type, Equals, testJobType)
	c.Assert(job.AppID, Equals, app.ID)
	c.Assert(job.ReleaseID, Equals, testReleaseID)
	_, err = waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	jobs := s.Jobs()
	c.Assert(jobs, HasLen, 4)

	// Test scaling down an existing formation
	c.Log("Test scaling down an existing formation. Wait for formation change and job stop")
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: map[string]int{"web": 1}})
	_, err = waitForEvent(events, EventTypeFormationChange)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeJobStop)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeJobStop)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeJobStop)
	c.Assert(err, IsNil)
	jobs = s.Jobs()
	c.Assert(jobs, HasLen, 1)

	// Test creating a new formation
	c.Log("Test creating a new formation. Wait for formation change and job start")
	artifact = &ct.Artifact{ID: random.UUID()}
	processes := map[string]int{testJobType: testJobCount}
	release = NewRelease(random.UUID(), artifact, processes)
	s.CreateArtifact(artifact)
	s.CreateRelease(release)
	c.Assert(len(s.formations), Equals, 1)
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	_, err = waitForEvent(events, EventTypeFormationChange)
	c.Assert(err, IsNil)
	c.Assert(len(s.formations), Equals, 2)
	e, err = waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	job = checkJobStartEvent(c, e)
	c.Assert(job.Type, Equals, testJobType)
	c.Assert(job.AppID, Equals, app.ID)
	c.Assert(job.ReleaseID, Equals, release.ID)
}

func (ts *TestSuite) TestRectify(c *C) {
	h := NewFakeHostClient(testHostID)
	cluster := NewFakeCluster()
	cluster.SetHosts(map[string]*FakeHostClient{h.ID(): h})
	events := make(chan Event, eventBufferSize)
	sched := runTestScheduler(cluster, events, true)
	defer sched.Stop()
	s := sched.scheduler

	// wait for the formation to cascade to the scheduler
	_, err := waitForEvent(events, EventTypeRectify)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	jobs := s.Jobs()
	c.Assert(jobs, HasLen, 1)

	// Create an extra job on a host and wait for it to start
	c.Log("Test creating an extra job on the host. Wait for job start in scheduler")
	form := s.formations.Get(testAppID, testReleaseID)
	host, err := s.Host(testHostID)
	request := NewJobRequest(form, JobRequestTypeUp, testJobType, "", "")
	config := jobConfig(request, testHostID)
	host.AddJob(config)
	_, err = waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	jobs = s.Jobs()
	c.Assert(jobs, HasLen, 2)

	// Verify that the scheduler stops the extra job
	c.Log("Verify that the scheduler stops the extra job")
	_, err = waitForEvent(events, EventTypeRectify)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeJobStop)
	c.Assert(err, IsNil)
	jobs = s.Jobs()
	c.Assert(jobs, HasLen, 1)
	_, ok := jobs[config.ID]
	c.Assert(ok, Equals, false)

	// Create a new app, artifact, release, and associated formation
	c.Log("Create a new app, artifact, release, and associated formation")
	app := &ct.App{ID: "test-app-2", Name: "test-app-2"}
	artifact := &ct.Artifact{ID: "test-artifact-2"}
	processes := map[string]int{testJobType: testJobCount}
	release := NewRelease("test-release-2", artifact, processes)
	form = NewFormation(&ct.ExpandedFormation{App: app, Release: release, Artifact: artifact, Processes: processes})
	request = NewJobRequest(form, JobRequestTypeUp, testJobType, "", "")
	config = jobConfig(request, testHostID)
	// Add the job to the host without adding the formation. Expected error.
	c.Log("Create a new job on the host without adding the formation to the controller. Wait for job start, expect error.")
	host.AddJob(config)
	_, err = waitForEvent(events, EventTypeJobStart)
	c.Assert(err, Not(IsNil))

	c.Log("Add the formation to the controller. Wait for formation change.")
	s.CreateApp(app)
	s.CreateArtifact(artifact)
	s.CreateRelease(release)
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	_, err = waitForEvent(events, EventTypeFormationChange)
	c.Assert(err, IsNil)
	jobs = s.Jobs()
	c.Assert(jobs, HasLen, 2)
}

func (ts *TestSuite) TestExponentialBackoffNoHosts(c *C) {
	cluster := NewFakeCluster()
	events := make(chan Event, eventBufferSize)
	sched := runTestScheduler(cluster, events, true)
	defer sched.Stop()

	// wait for the formation to cascade to the scheduler
	_, err := waitForEvent(events, EventTypeRectify)
	c.Assert(err, IsNil)
	evt, err := waitForEvent(events, EventTypeJobRequest)
	c.Assert(err.Error(), Equals, "unexpected event error: no hosts found")
	req := checkJobRequestEvent(c, evt)
	c.Assert(req.restarts, Equals, uint(1))
	evt, err = waitDurationForEvent(events, EventTypeJobRequest, 1*time.Second+50*time.Millisecond)
	c.Assert(err.Error(), Equals, "unexpected event error: no hosts found")
	req = checkJobRequestEvent(c, evt)
	c.Assert(req.restarts, Equals, uint(2))
	evt, err = waitDurationForEvent(events, EventTypeJobRequest, 2*time.Second+50*time.Millisecond)
	c.Assert(err.Error(), Equals, "unexpected event error: no hosts found")
	req = checkJobRequestEvent(c, evt)
	c.Assert(req.restarts, Equals, uint(3))
}

func (ts *TestSuite) TestMultipleHosts(c *C) {
	h := NewFakeHostClient(testHostID)
	hosts := map[string]*FakeHostClient{
		h.ID(): h,
	}
	cluster := NewFakeCluster()
	cluster.SetHosts(hosts)
	events := make(chan Event, eventBufferSize)
	sched := runTestScheduler(cluster, events, true)
	defer sched.Stop()
	s := sched.scheduler
	c.Log("Initialize the cluster with 1 host and wait for a job to start on it.")
	_, err := waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)

	c.Log("Add a host to the cluster, then create a new app, artifact, release, and associated formation.")
	h2 := NewFakeHostClient("host-2")
	cluster.AddHost(h2)
	hosts[h2.ID()] = h2
	app := &ct.App{ID: "test-app-2", Name: "test-app-2"}
	artifact := &ct.Artifact{ID: "test-artifact-2"}
	processes := map[string]int{testJobType: 1}
	release := NewReleaseOmni("test-release-2", artifact, processes, true)
	c.Log("Add the formation to the controller. Wait for formation change and job start on both hosts.")
	s.CreateApp(app)
	s.CreateArtifact(artifact)
	s.CreateRelease(release)
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	_, err = waitForEvent(events, EventTypeFormationChange)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	jobs := s.Jobs()
	c.Assert(jobs, HasLen, 3)

	hostJobs, err := h.ListJobs()
	c.Assert(err, IsNil)
	c.Assert(len(hostJobs), Equals, 2)
	hostJobs, err = h2.ListJobs()
	c.Assert(err, IsNil)
	c.Assert(len(hostJobs), Equals, 1)

	h3 := NewFakeHostClient("host-3")
	c.Log("Add a host, wait for omni job start on that host.")
	cluster.AddHost(h3)
	_, err = waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	jobs = s.Jobs()
	c.Assert(jobs, HasLen, 4)
	hostJobs, err = h3.ListJobs()
	c.Assert(err, IsNil)
	c.Assert(len(hostJobs), Equals, 1)

	c.Log("Crash one of the omni jobs, and wait for it to restart")
	for id := range hostJobs {
		h3.CrashJob(id)
	}
	hostJobs, err = h3.ListJobs()
	c.Assert(err, IsNil)
	c.Assert(len(hostJobs), Equals, 0)
	_, err = waitForEvent(events, EventTypeJobStop)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeJobRequest)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	hostJobs, err = h3.ListJobs()
	c.Assert(err, IsNil)
	c.Assert(len(hostJobs), Equals, 1)

	c.Logf("Remove one of the hosts. Ensure the cluster recovers correctly (hosts=%v)", hosts)
	cluster.SetHosts(hosts)
	_, err = waitForEvent(events, EventTypeFormationSync)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeRectify)
	c.Assert(err, IsNil)
	jobs = s.Jobs()
	c.Assert(jobs, HasLen, 3)
	hostJobs, err = h.ListJobs()
	c.Assert(err, IsNil)
	c.Assert(len(hostJobs), Equals, 2)
	hostJobs, err = h2.ListJobs()
	c.Assert(err, IsNil)
	c.Assert(len(hostJobs), Equals, 1)

	c.Logf("Remove another host. Ensure the cluster recovers correctly (hosts=%v)", hosts)
	cluster.RemoveHost(testHostID)
	_, err = waitForEvent(events, EventTypeFormationSync)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeRectify)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	jobs = s.Jobs()
	c.Assert(jobs, HasLen, 2)
	hostJobs, err = h2.ListJobs()
	c.Assert(err, IsNil)
	c.Assert(len(hostJobs), Equals, 2)
}

func (ts *TestSuite) TestMultipleSchedulers(c *C) {
	// Set up cluster and both schedulers
	h := NewFakeHostClient(testHostID)
	hosts := map[string]*FakeHostClient{
		h.ID(): h,
	}
	cluster := NewFakeCluster()
	cluster.SetHosts(hosts)
	events1 := make(chan Event, eventBufferSize)
	sched1 := runTestScheduler(cluster, events1, false)
	defer sched1.Stop()
	s1 := sched1.scheduler
	events2 := make(chan Event, eventBufferSize)
	sched2 := runTestScheduler(cluster, events2, false)
	defer sched2.Stop()
	s2 := sched2.scheduler
	_, err := waitDurationForEvent(events1, EventTypeJobStart, 1*time.Second)
	c.Assert(err, Not(IsNil))
	_, err = waitDurationForEvent(events2, EventTypeJobStart, 1*time.Second)
	c.Assert(err, Not(IsNil))

	// Make S1 the leader, wait for jobs to start
	s1.ChangeLeader(true)
	_, err = waitForEvent(events1, EventTypeJobStart)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events2, EventTypeJobStart)
	c.Assert(err, IsNil)
	jobs := s1.Jobs()
	c.Assert(jobs, HasLen, 1)
	jobs = s2.Jobs()
	c.Assert(jobs, HasLen, 1)

	s1.ChangeLeader(false)

	app, err := s2.GetApp(testAppID)
	c.Assert(err, IsNil)
	release, err := s2.GetRelease(testReleaseID)
	c.Assert(err, IsNil)

	// Test scaling up an existing formation
	form := &ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: map[string]int{"web": 2}}
	c.Log("Test scaling up an existing formation. Wait for formation change and job start")
	s1.PutFormation(form)
	s2.PutFormation(form)
	_, err = waitForEvent(events1, EventTypeFormationChange)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events2, EventTypeFormationChange)
	c.Assert(err, IsNil)
	_, err = waitDurationForEvent(events2, EventTypeJobStart, 1*time.Second)
	c.Assert(err, Not(IsNil))
	_, err = waitDurationForEvent(events1, EventTypeJobStart, 1*time.Second)
	c.Assert(err, Not(IsNil))

	s2.ChangeLeader(true)

	_, err = waitForEvent(events2, EventTypeJobStart)
	c.Assert(err, IsNil)
	_, err = waitForEvent(events1, EventTypeJobStart)
	c.Assert(err, IsNil)
}

func checkJobStartEvent(c *C, e Event) *Job {
	event, ok := e.(*JobStartEvent)
	c.Assert(ok, Equals, true)
	c.Assert(event.Job, NotNil)
	return event.Job
}

func checkJobRequestEvent(c *C, e Event) *JobRequest {
	event, ok := e.(*JobRequestEvent)
	c.Assert(ok, Equals, true)
	c.Assert(event.Request, NotNil)
	return event.Request
}
