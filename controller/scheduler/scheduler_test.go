package main

import (
	"fmt"
	"testing"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	. "github.com/flynn/flynn/controller/testutils"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/stream"
)

func Test(t *testing.T) { TestingT(t) }

type TestSuite struct{}

var _ = Suite(&TestSuite{})

const (
	testAppID      = "app-1"
	testHostID     = "host1"
	testArtifactId = "artifact-1"
	testReleaseID  = "release-1"
	testJobType    = "web"
	testJobCount   = 1
)

func newFakeDiscoverd(firstLeader bool) *fakeDiscoverd {
	return &fakeDiscoverd{
		firstLeader: firstLeader,
		leader:      make(chan bool, 1),
	}
}

type fakeDiscoverd struct {
	firstLeader bool
	leader      chan bool
}

func (d *fakeDiscoverd) Register() (bool, error) {
	return d.firstLeader, nil
}

func (d *fakeDiscoverd) LeaderCh() chan bool {
	return d.leader
}

func (d *fakeDiscoverd) promote() {
	d.leader <- true
}

func (d *fakeDiscoverd) demote() {
	d.leader <- false
}

func createTestScheduler(cluster utils.ClusterClient, discoverd Discoverd) *Scheduler {
	app := &ct.App{ID: testAppID, Name: testAppID}
	artifact := &ct.Artifact{ID: testArtifactId}
	processes := map[string]int{testJobType: testJobCount}
	release := NewRelease(testReleaseID, artifact, processes)
	cc := NewFakeControllerClient()
	cc.CreateApp(app)
	cc.CreateArtifact(artifact)
	cc.CreateRelease(release)
	cc.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	return NewScheduler(cluster, cc, discoverd)
}

func newTestHosts() map[string]*FakeHostClient {
	return map[string]*FakeHostClient{
		testHostID: NewFakeHostClient(testHostID),
	}
}

func newTestCluster(hosts map[string]*FakeHostClient) *FakeCluster {
	cluster := NewFakeCluster()
	if hosts == nil {
		hosts = newTestHosts()
	}
	cluster.SetHosts(hosts)
	return cluster
}

func runTestScheduler(c *C, cluster utils.ClusterClient, isLeader bool) *TestScheduler {
	if cluster == nil {
		cluster = newTestCluster(nil)
	}
	discoverd := newFakeDiscoverd(isLeader)
	s := createTestScheduler(cluster, discoverd)

	events := make(chan Event, eventBufferSize)
	stream := s.Subscribe(events)
	go s.Run()

	return &TestScheduler{s, c, events, stream, discoverd}
}

type TestScheduler struct {
	*Scheduler
	c         *C
	events    chan Event
	stream    stream.Stream
	discoverd *fakeDiscoverd
}

func (s *TestScheduler) Stop() {
	s.Scheduler.Stop()
	s.stream.Close()
}

func (s *TestScheduler) waitRectify() utils.FormationKey {
	event, err := s.waitForEvent(EventTypeRectify)
	s.c.Assert(err, IsNil)
	e, ok := event.(*RectifyEvent)
	if !ok {
		s.c.Fatalf("expected RectifyEvent, got %T", e)
	}
	s.c.Assert(e.FormationKey, NotNil)
	return e.FormationKey
}

func (s *TestScheduler) waitFormationChange() {
	_, err := s.waitForEvent(EventTypeFormationChange)
	s.c.Assert(err, IsNil)
}

func (s *TestScheduler) waitFormationSync() {
	_, err := s.waitForEvent(EventTypeFormationSync)
	s.c.Assert(err, IsNil)
}

func (s *TestScheduler) waitJobStart() *Job {
	return s.waitJobEvent(EventTypeJobStart)
}

func (s *TestScheduler) waitJobStop() *Job {
	return s.waitJobEvent(EventTypeJobStop)
}

func (s *TestScheduler) waitJobEvent(typ EventType) *Job {
	event, err := s.waitForEvent(typ)
	s.c.Assert(err, IsNil)
	e, ok := event.(*JobEvent)
	if !ok {
		s.c.Fatalf("expected JobEvent, got %T", e)
	}
	s.c.Assert(e.Job, NotNil)
	return e.Job
}

func (s *TestScheduler) waitDurationForEvent(typ EventType, duration time.Duration) (Event, error) {
	for {
		select {
		case event, ok := <-s.events:
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

func (s *TestScheduler) waitForEvent(typ EventType) (Event, error) {
	return s.waitDurationForEvent(typ, 2*time.Second)
}

func (TestSuite) TestSingleJobStart(c *C) {
	s := runTestScheduler(c, nil, true)
	defer s.Stop()

	// wait for a rectify jobs event
	job := s.waitJobStart()
	c.Assert(job.Type, Equals, testJobType)
	c.Assert(job.AppID, Equals, testAppID)
	c.Assert(job.ReleaseID, Equals, testReleaseID)

	// Query the scheduler for the same job
	c.Log("Verify that the scheduler has the same job")
	jobs := s.RunningJobs()
	c.Assert(jobs, HasLen, 1)
	for _, job := range jobs {
		c.Assert(job.Type, Equals, testJobType)
		c.Assert(job.HostID, Equals, testHostID)
		c.Assert(job.AppID, Equals, testAppID)
	}
}

func (TestSuite) TestFormationChange(c *C) {
	s := runTestScheduler(c, nil, true)
	defer s.Stop()

	s.waitJobStart()

	app, err := s.GetApp(testAppID)
	c.Assert(err, IsNil)
	release, err := s.GetRelease(testReleaseID)
	c.Assert(err, IsNil)
	artifact, err := s.GetArtifact(release.ArtifactID)
	c.Assert(err, IsNil)

	// Test scaling up an existing formation
	c.Log("Test scaling up an existing formation. Wait for formation change and job start")
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: map[string]int{"web": 4}})
	s.waitFormationChange()
	for i := 0; i < 3; i++ {
		job := s.waitJobStart()
		c.Assert(job.Type, Equals, testJobType)
		c.Assert(job.AppID, Equals, app.ID)
		c.Assert(job.ReleaseID, Equals, testReleaseID)
	}
	c.Assert(s.RunningJobs(), HasLen, 4)

	// Test scaling down an existing formation
	c.Log("Test scaling down an existing formation. Wait for formation change and job stop")
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: map[string]int{"web": 1}})
	s.waitFormationChange()
	for i := 0; i < 3; i++ {
		s.waitJobStop()
	}
	c.Assert(s.RunningJobs(), HasLen, 1)

	// Test creating a new formation
	c.Log("Test creating a new formation. Wait for formation change and job start")
	artifact = &ct.Artifact{ID: random.UUID()}
	processes := map[string]int{testJobType: testJobCount}
	release = NewRelease(random.UUID(), artifact, processes)
	s.CreateArtifact(artifact)
	s.CreateRelease(release)
	c.Assert(len(s.formations), Equals, 1)
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	s.waitFormationChange()
	c.Assert(len(s.formations), Equals, 2)
	job := s.waitJobStart()
	c.Assert(job.Type, Equals, testJobType)
	c.Assert(job.AppID, Equals, app.ID)
	c.Assert(job.ReleaseID, Equals, release.ID)
}

func (TestSuite) TestRectify(c *C) {
	s := runTestScheduler(c, nil, true)
	defer s.Stop()

	// wait for the formation to cascade to the scheduler
	key := s.waitRectify()
	job := s.waitJobStart()
	jobs := make(map[string]*Job)
	jobs[job.JobID] = job
	c.Assert(jobs, HasLen, 1)
	c.Assert(job.Formation.key(), Equals, key)

	// Create an extra job on a host and wait for it to start
	c.Log("Test creating an extra job on the host. Wait for job start in scheduler")
	form := s.formations.Get(testAppID, testReleaseID)
	host, _ := s.Host(testHostID)
	newJob := &Job{Formation: form, AppID: testAppID, ReleaseID: testReleaseID, Type: testJobType}
	config := jobConfig(newJob, testHostID)
	host.AddJob(config)
	job = s.waitJobStart()
	jobs[job.JobID] = job
	c.Assert(jobs, HasLen, 2)

	// Verify that the scheduler stops the extra job
	c.Log("Verify that the scheduler stops the extra job")
	s.waitRectify()
	job = s.waitJobStop()
	c.Assert(job.JobID, Equals, config.ID)
	delete(jobs, job.JobID)
	c.Assert(jobs, HasLen, 1)

	// Create a new app, artifact, release, and associated formation
	c.Log("Create a new app, artifact, release, and associated formation")
	app := &ct.App{ID: "test-app-2", Name: "test-app-2"}
	artifact := &ct.Artifact{ID: "test-artifact-2"}
	processes := map[string]int{testJobType: testJobCount}
	release := NewRelease("test-release-2", artifact, processes)
	form = NewFormation(&ct.ExpandedFormation{App: app, Release: release, Artifact: artifact, Processes: processes})
	newJob = &Job{Formation: form, AppID: testAppID, ReleaseID: testReleaseID, Type: testJobType}
	config = jobConfig(newJob, testHostID)
	// Add the job to the host without adding the formation. Expected error.
	c.Log("Create a new job on the host without adding the formation to the controller. Wait for job start, expect job with nil formation.")
	host.AddJob(config)
	job = s.waitJobStart()
	c.Assert(job.Formation, IsNil)

	c.Log("Add the formation to the controller. Wait for formation change. Check the job has a formation and no new job was created")
	s.CreateApp(app)
	s.CreateArtifact(artifact)
	s.CreateRelease(release)
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	s.waitFormationChange()
	_, err := s.waitDurationForEvent(EventTypeJobStart, 1*time.Second)
	c.Assert(err, NotNil)
	c.Assert(job.Formation, NotNil)
	c.Assert(s.RunningJobs(), HasLen, 2)
}

func (TestSuite) TestMultipleHosts(c *C) {
	hosts := newTestHosts()
	fakeCluster := newTestCluster(hosts)
	s := runTestScheduler(c, fakeCluster, true)
	defer s.Stop()
	s.maxHostChecks = 1

	// use incremental job IDs so we can find them easily in s.jobs
	jobID := 0
	generateJobUUID = func() string {
		jobID++
		return fmt.Sprintf("job%d", jobID)
	}
	defer func() { generateJobUUID = random.UUID }()

	assertJobs := func(expected map[string]*Job) {
		jobs := s.Jobs()
		c.Assert(jobs, HasLen, len(expected))
		for id, job := range expected {
			actual, ok := jobs[id]
			if !ok {
				c.Fatalf("%s does not exist in s.jobs", id)
			}
			c.Assert(actual.Type, Equals, job.Type)
			c.Assert(actual.state, Equals, job.state)
			c.Assert(actual.HostID, Equals, job.HostID)
		}
	}

	c.Log("Initialize the cluster with 1 host and wait for a job to start on it.")
	s.waitJobStart()
	assertJobs(map[string]*Job{
		"job1": {Type: "web", state: JobStateStarting, HostID: testHostID},
	})

	c.Log("Add a host to the cluster, then create a new app, artifact, release, and associated formation.")
	h2 := NewFakeHostClient("host2")
	fakeCluster.AddHost(h2)
	hosts[h2.ID()] = h2
	app := &ct.App{ID: "test-app-2", Name: "test-app-2"}
	artifact := &ct.Artifact{ID: "test-artifact-2"}
	processes := map[string]int{"omni": 1}
	release := NewReleaseOmni("test-release-2", artifact, processes, true)
	c.Log("Add the formation to the controller. Wait for formation change and job start on both hosts.")
	s.CreateApp(app)
	s.CreateArtifact(artifact)
	s.CreateRelease(release)
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	s.waitFormationChange()
	s.waitJobStart()
	s.waitJobStart()
	assertJobs(map[string]*Job{
		"job1": {Type: "web", state: JobStateStarting, HostID: "host1"},
		"job2": {Type: "omni", state: JobStateStarting, HostID: "host1"},
		"job3": {Type: "omni", state: JobStateStarting, HostID: "host2"},
	})

	assertHostJobs := func(host *FakeHostClient, ids ...string) {
		jobs, err := host.ListJobs()
		c.Assert(err, IsNil)
		c.Assert(jobs, HasLen, len(ids))
		for _, id := range ids {
			id = cluster.GenerateJobID(host.ID(), id)
			job, ok := jobs[id]
			if !ok {
				c.Fatalf("%s missing job with ID %s", host.ID(), id)
			}
			c.Assert(job.Job.ID, Equals, id)
		}
	}
	h1 := hosts[testHostID]
	assertHostJobs(h1, "job1", "job2")
	assertHostJobs(h2, "job3")

	h3 := NewFakeHostClient("host3")
	c.Log("Add a host, wait for omni job start on that host.")
	fakeCluster.AddHost(h3)
	s.waitJobStart()
	assertJobs(map[string]*Job{
		"job1": {Type: "web", state: JobStateStarting, HostID: "host1"},
		"job2": {Type: "omni", state: JobStateStarting, HostID: "host1"},
		"job3": {Type: "omni", state: JobStateStarting, HostID: "host2"},
		"job4": {Type: "omni", state: JobStateStarting, HostID: "host3"},
	})
	assertHostJobs(h3, "job4")

	c.Log("Crash one of the omni jobs, and wait for it to restart")
	h3.CrashJob("job4")
	s.waitJobStop()
	s.waitJobStart()
	assertJobs(map[string]*Job{
		"job1": {Type: "web", state: JobStateStarting, HostID: "host1"},
		"job2": {Type: "omni", state: JobStateStarting, HostID: "host1"},
		"job3": {Type: "omni", state: JobStateStarting, HostID: "host2"},
		"job4": {Type: "omni", state: JobStateStopped, HostID: "host3"},
		"job5": {Type: "omni", state: JobStateStarting, HostID: "host3"},
	})
	assertHostJobs(h3, "job5")

	c.Logf("Remove one of the hosts. Ensure the cluster recovers correctly (hosts=%v)", hosts)
	h3.Healthy = false
	fakeCluster.SetHosts(hosts)
	s.waitFormationSync()
	s.waitRectify()
	assertJobs(map[string]*Job{
		"job1": {Type: "web", state: JobStateStarting, HostID: "host1"},
		"job2": {Type: "omni", state: JobStateStarting, HostID: "host1"},
		"job3": {Type: "omni", state: JobStateStarting, HostID: "host2"},
		"job4": {Type: "omni", state: JobStateStopped, HostID: "host3"},
		"job5": {Type: "omni", state: JobStateStopped, HostID: "host3"},
	})
	assertHostJobs(h1, "job1", "job2")
	assertHostJobs(h2, "job3")

	c.Logf("Remove another host. Ensure the cluster recovers correctly (hosts=%v)", hosts)
	h1.Healthy = false
	fakeCluster.RemoveHost(testHostID)
	s.waitFormationSync()
	s.waitRectify()
	s.waitJobStart()
	assertJobs(map[string]*Job{
		"job1": {Type: "web", state: JobStateStopped, HostID: "host1"},
		"job2": {Type: "omni", state: JobStateStopped, HostID: "host1"},
		"job3": {Type: "omni", state: JobStateStarting, HostID: "host2"},
		"job4": {Type: "omni", state: JobStateStopped, HostID: "host3"},
		"job5": {Type: "omni", state: JobStateStopped, HostID: "host3"},
		"job6": {Type: "web", state: JobStateStarting, HostID: "host2"},
	})
	assertHostJobs(h2, "job3", "job6")
}

func (TestSuite) TestMultipleSchedulers(c *C) {
	// Set up cluster and both schedulers
	cluster := newTestCluster(nil)
	s1 := runTestScheduler(c, cluster, false)
	defer s1.Stop()
	s2 := runTestScheduler(c, cluster, false)
	defer s2.Stop()

	_, err := s1.waitDurationForEvent(EventTypeJobStart, 1*time.Second)
	c.Assert(err, Not(IsNil))
	_, err = s2.waitDurationForEvent(EventTypeJobStart, 1*time.Second)
	c.Assert(err, Not(IsNil))

	// Make S1 the leader, wait for jobs to start
	s1.discoverd.promote()
	s1.waitJobStart()
	s2.waitJobStart()
	c.Assert(s1.RunningJobs(), HasLen, 1)
	c.Assert(s2.RunningJobs(), HasLen, 1)

	s1.discoverd.demote()

	app, err := s2.GetApp(testAppID)
	c.Assert(err, IsNil)
	release, err := s2.GetRelease(testReleaseID)
	c.Assert(err, IsNil)

	// Test scaling up an existing formation
	formation := &ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: map[string]int{"web": 2}}
	c.Log("Test scaling up an existing formation. Wait for formation change and job start")
	s1.PutFormation(formation)
	s2.PutFormation(formation)
	s1.waitFormationChange()
	s2.waitFormationChange()
	_, err = s2.waitDurationForEvent(EventTypeJobStart, 1*time.Second)
	c.Assert(err, Not(IsNil))
	_, err = s1.waitDurationForEvent(EventTypeJobStart, 1*time.Second)
	c.Assert(err, Not(IsNil))

	s2.discoverd.promote()
	s2.waitJobStart()
	s1.waitJobStart()
}

func (TestSuite) TestStopJob(c *C) {
	s := &Scheduler{}
	formation := NewFormation(&ct.ExpandedFormation{
		App:     &ct.App{ID: "app"},
		Release: &ct.Release{ID: "release"},
	})
	otherFormation := NewFormation(&ct.ExpandedFormation{
		App:     &ct.App{ID: "other_app"},
		Release: &ct.Release{ID: "other_release"},
	})
	recent := time.Now()

	type test struct {
		desc       string
		jobs       Jobs
		shouldStop string
		err        string
		jobCheck   func(*Job)
	}
	for _, t := range []*test{
		{
			desc: "no jobs running",
			jobs: nil,
			err:  "no web jobs running",
		},
		{
			desc: "no jobs from formation running",
			jobs: Jobs{"job1": &Job{ID: "job1", Formation: otherFormation}},
			err:  "no web jobs running",
		},
		{
			desc: "no jobs with type running",
			jobs: Jobs{"job1": &Job{ID: "job1", Formation: formation, Type: "worker"}},
			err:  "no web jobs running",
		},
		{
			desc:       "a running job",
			jobs:       Jobs{"job1": &Job{ID: "job1", Formation: formation, Type: "web", state: JobStateRunning}},
			shouldStop: "job1",
		},
		{
			desc: "multiple running jobs, stops most recent",
			jobs: Jobs{
				"job1": &Job{ID: "job1", Formation: formation, Type: "web", state: JobStateRunning, startedAt: recent.Add(-5 * time.Minute)},
				"job2": &Job{ID: "job2", Formation: formation, Type: "web", state: JobStateRunning, startedAt: recent},
				"job3": &Job{ID: "job3", Formation: formation, Type: "web", state: JobStateRunning, startedAt: recent.Add(-10 * time.Minute)},
			},
			shouldStop: "job2",
		},
		{
			desc: "one running and one stopped, stops running job",
			jobs: Jobs{
				"job1": &Job{ID: "job1", Formation: formation, Type: "web", state: JobStateRunning, startedAt: recent.Add(-5 * time.Minute)},
				"job2": &Job{ID: "job2", Formation: formation, Type: "web", state: JobStateStopped, startedAt: recent},
			},
			shouldStop: "job1",
		},
		{
			desc: "one running and one scheduled, stops scheduled job",
			jobs: Jobs{
				"job1": &Job{ID: "job1", Formation: formation, Type: "web", state: JobStatePending, startedAt: recent.Add(-5 * time.Minute), restartTimer: time.NewTimer(0)},
				"job2": &Job{ID: "job2", Formation: formation, Type: "web", state: JobStateRunning, startedAt: recent},
			},
			shouldStop: "job1",
			jobCheck:   func(job *Job) { c.Assert(job.state, Equals, JobStateStopped) },
		},
		{
			desc: "one running and one new, stops new job",
			jobs: Jobs{
				"job1": &Job{ID: "job1", Formation: formation, Type: "web", state: JobStatePending, startedAt: recent.Add(-5 * time.Minute)},
				"job2": &Job{ID: "job2", Formation: formation, Type: "web", state: JobStateRunning, startedAt: recent},
			},
			shouldStop: "job1",
			jobCheck:   func(job *Job) { c.Assert(job.state, Equals, JobStateStopped) },
		},
	} {
		s.jobs = t.jobs
		job, err := s.findJobToStop(formation, "web")
		if t.err != "" {
			c.Assert(err, NotNil, Commentf(t.desc))
			c.Assert(err.Error(), Equals, t.err, Commentf(t.desc))
			continue
		}
		c.Assert(job.ID, Equals, t.shouldStop, Commentf(t.desc))
		if t.jobCheck != nil {
			t.jobCheck(job)
		}
	}
}
