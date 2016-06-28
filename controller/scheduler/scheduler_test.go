package main

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/flynn/flynn/controller/testutils"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/typeconv"
	. "github.com/flynn/go-check"
	"gopkg.in/inconshreveable/log15.v2"
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

func (d *fakeDiscoverd) Register() bool {
	return d.firstLeader
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

func createTestScheduler(cluster utils.ClusterClient, discoverd Discoverd, processes map[string]int, l log15.Logger) *Scheduler {
	app := &ct.App{ID: testAppID, Name: testAppID}
	artifact := &ct.Artifact{ID: testArtifactId}
	if processes == nil {
		processes = map[string]int{testJobType: testJobCount}
	}
	release := NewRelease(testReleaseID, artifact, processes)
	cc := NewFakeControllerClient()
	cc.CreateApp(app)
	cc.CreateArtifact(artifact)
	cc.CreateRelease(app.ID, release)
	cc.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	return NewScheduler(cluster, cc, discoverd, l)
}

func newTestHosts() map[string]utils.HostClient {
	return map[string]utils.HostClient{
		testHostID: NewFakeHostClient(testHostID, false),
	}
}

func newTestCluster(hosts map[string]utils.HostClient) *FakeCluster {
	cluster := NewFakeCluster()
	if hosts == nil {
		hosts = newTestHosts()
	}
	cluster.SetHosts(hosts)
	return cluster
}

func newTestScheduler(c *C, cluster utils.ClusterClient, isLeader bool, processes map[string]int) *TestScheduler {
	if cluster == nil {
		cluster = newTestCluster(nil)
	}
	discoverd := newFakeDiscoverd(isLeader)

	events := make(chan *log15.Record, eventBufferSize)
	logger := log15.New()
	logger.SetHandler(log15.MultiHandler(
		log15.StdoutHandler,
		log15.ChannelHandler(events),
	))

	s := createTestScheduler(cluster, discoverd, processes, logger)
	return &TestScheduler{s, c, events, discoverd}
}

func runTestScheduler(c *C, cluster utils.ClusterClient, isLeader bool) *TestScheduler {
	s := newTestScheduler(c, cluster, isLeader, nil)
	go s.Run()
	return s
}

type logEvent struct {
	log15.Record
}

func (l *logEvent) Get(key string) interface{} {
	for i := 0; i < len(l.Record.Ctx); i += 2 {
		if k, ok := l.Record.Ctx[i].(string); ok && k == key {
			return l.Record.Ctx[i+1]
		}
	}
	return nil
}

type TestScheduler struct {
	*Scheduler
	c         *C
	events    chan *log15.Record
	discoverd *fakeDiscoverd
}

func (s *TestScheduler) Stop() {
	s.Scheduler.Stop()
}

func (s *TestScheduler) waitRectify() utils.FormationKey {
	event, err := s.waitForEvent("rectified formation", nil)
	s.c.Assert(err, IsNil)
	return event.Get("key").(utils.FormationKey)
}

func (s *TestScheduler) waitFormationChange() {
	_, err := s.waitForEvent("formation change handled", nil)
	s.c.Assert(err, IsNil)
}

func (s *TestScheduler) waitFormationSync() {
	_, err := s.waitForEvent("formations synced", nil)
	s.c.Assert(err, IsNil)
}

func (s *TestScheduler) waitForHost() {
	_, err := s.waitForEvent("handled host event", nil)
	s.c.Assert(err, IsNil)
}

func (s *TestScheduler) waitJobStart() *Job {
	return s.waitJobEvent("start", nil)
}

func (s *TestScheduler) waitJobStartWithErr(expectedErr string) *Job {
	return s.waitJobEvent("start", &expectedErr)
}

func (s *TestScheduler) waitJobStop() *Job {
	return s.waitJobEvent("stop", nil)
}

func (s *TestScheduler) waitJobEvent(typ string, expectedErr *string) *Job {
	event, err := s.waitForEvent(fmt.Sprintf("handled job %s event", typ), expectedErr)
	s.c.Assert(err, IsNil)
	return event.Get("job").(*Job)
}

func (s *TestScheduler) waitDurationForEvent(msg string, duration time.Duration, expectedErr *string) (*logEvent, error) {
	for {
		select {
		case event, ok := <-s.events:
			if !ok {
				return nil, errors.New("unexpected close of scheduler event stream")
			}
			if event.Lvl <= log15.LvlError {
				if expectedErr == nil || event.Msg != *expectedErr {
					return nil, fmt.Errorf("unexpected event: %s: %s", event.Lvl, event.Msg)
				}
			}
			if event.Msg == msg {
				return &logEvent{*event}, nil
			}
		case <-time.After(duration):
			return nil, fmt.Errorf("timed out waiting for event: %q", msg)
		}
	}
}

func (s *TestScheduler) waitForError(msg string) {
	_, err := s.waitForEvent(msg, &msg)
	s.c.Assert(err, IsNil)
}

func (s *TestScheduler) waitForEvent(msg string, expectedErr *string) (*logEvent, error) {
	return s.waitDurationForEvent(msg, 2*time.Second, expectedErr)
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
	artifact, err := s.GetArtifact(release.ArtifactIDs[0])
	c.Assert(err, IsNil)

	// Test scaling up an existing formation
	c.Log("Test scaling up an existing formation. Wait for formation change and job start")
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: map[string]int{"web": 4}})
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
	s.CreateRelease(app.ID, release)
	c.Assert(len(s.formations), Equals, 1)
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	job := s.waitJobStart()
	c.Assert(len(s.formations), Equals, 2)
	c.Assert(job.Type, Equals, testJobType)
	c.Assert(job.AppID, Equals, app.ID)
	c.Assert(job.ReleaseID, Equals, release.ID)
}

func (TestSuite) TestRectify(c *C) {
	s := runTestScheduler(c, nil, true)
	defer s.Stop()

	// wait for the formation to cascade to the scheduler
	key := utils.FormationKey{AppID: testAppID, ReleaseID: testReleaseID}
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
	form = NewFormation(&ct.ExpandedFormation{App: app, Release: release, Artifacts: []*ct.Artifact{artifact}, Processes: processes})
	newJob = &Job{Formation: form, AppID: testAppID, ReleaseID: testReleaseID, Type: testJobType}
	config = jobConfig(newJob, testHostID)
	// Add the job to the host without adding the formation. Expected error.
	c.Log("Create a new job on the host without adding the formation to the controller. Wait for job start, expect job with nil formation.")
	host.AddJob(config)
	job = s.waitJobStartWithErr("error looking up formation for job")
	c.Assert(job.Formation, IsNil)

	c.Log("Add the formation to the controller. Wait for formation change. Check the job has a formation and no new job was created")
	s.CreateApp(app)
	s.CreateArtifact(artifact)
	s.CreateRelease(app.ID, release)
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	s.waitFormationChange()
	_, err := s.waitDurationForEvent("handled job start event", 1*time.Second, nil)
	c.Assert(err, NotNil)
	c.Assert(job.Formation, NotNil)
	c.Assert(s.RunningJobs(), HasLen, 2)
}

func (TestSuite) TestMultipleHosts(c *C) {
	hosts := newTestHosts()
	host1 := hosts[testHostID]
	fakeCluster := newTestCluster(hosts)
	s := newTestScheduler(c, fakeCluster, true, nil)

	// use incremental job IDs so we can find them easily in s.jobs
	var jobID uint64
	s.generateJobUUID = func() string {
		return fmt.Sprintf("job%d", atomic.AddUint64(&jobID, 1))
	}
	s.maxHostChecks = 1

	go s.Run()
	defer s.Stop()

	// assertJobs checks that hosts have expected jobs based on their type
	// and current state
	type hostJobs map[utils.HostClient][]*Job
	assertJobs := func(expected hostJobs) {
		// get a sorted list of scheduler jobs per host to compare
		// against the expected list
		actual := make(map[string]sortJobs)
		for _, job := range s.InternalState().Jobs {
			actual[job.HostID] = append(actual[job.HostID], job)
		}
		for _, jobs := range actual {
			jobs.SortReverse()
		}

		for host, jobs := range expected {
			actual := actual[host.ID()]
			if len(actual) != len(jobs) {
				c.Fatalf("expected %s to have %d jobs, got %d", host.ID(), len(jobs), len(actual))
			}
			for i, job := range jobs {
				j := actual[i]
				c.Assert(j.Type, Equals, job.Type)
				c.Assert(j.State, Equals, job.State)

				// check the host has the job if it is running (stopped
				// jobs are removed from the host)
				if job.State != JobStateStarting {
					continue
				}
				id := cluster.GenerateJobID(host.ID(), j.ID)
				hostJob, err := host.GetJob(id)
				c.Assert(err, IsNil)
				c.Assert(hostJob.Job.ID, Equals, id)
			}
		}
	}

	c.Log("Initialize the cluster with 1 host and wait for a job to start on it.")
	s.waitJobStart()
	assertJobs(hostJobs{
		host1: {
			{Type: "web", State: JobStateStarting},
		},
	})

	c.Log("Add a host to the cluster, then create a new app, artifact, release, and associated formation.")
	host2 := NewFakeHostClient("host2", true)
	fakeCluster.AddHost(host2)
	hosts[host2.ID()] = host2
	app := &ct.App{ID: "test-app-2", Name: "test-app-2"}
	artifact := &ct.Artifact{ID: "test-artifact-2"}
	processes := map[string]int{"omni": 1}
	release := NewReleaseOmni("test-release-2", artifact, processes, true)
	c.Log("Add the formation to the controller. Wait for formation change and job start on both hosts.")
	s.CreateApp(app)
	s.CreateArtifact(artifact)
	s.CreateRelease(app.ID, release)
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	s.waitJobStart()
	s.waitJobStart()
	assertJobs(hostJobs{
		host1: {
			{Type: "web", State: JobStateStarting},
			{Type: "omni", State: JobStateStarting},
		},
		host2: {
			{Type: "omni", State: JobStateStarting},
		},
	})

	host3 := NewFakeHostClient("host3", true)
	c.Log("Add a host, wait for omni job start on that host.")
	fakeCluster.AddHost(host3)
	s.waitJobStart()
	assertJobs(hostJobs{
		host1: {
			{Type: "web", State: JobStateStarting},
			{Type: "omni", State: JobStateStarting},
		},
		host2: {
			{Type: "omni", State: JobStateStarting},
		},
		host3: {
			{Type: "omni", State: JobStateStarting},
		},
	})

	c.Log("Crash one of the omni jobs, and wait for it to restart")
	host3.CrashJob("job4")
	s.waitJobStop()
	s.waitJobStart()
	assertJobs(hostJobs{
		host1: {
			{Type: "web", State: JobStateStarting},
			{Type: "omni", State: JobStateStarting},
		},
		host2: {
			{Type: "omni", State: JobStateStarting},
		},
		host3: {
			{Type: "omni", State: JobStateStopped},
			{Type: "omni", State: JobStateStarting},
		},
	})

	c.Log("Unbalance the omni jobs, wait for them to be re-balanced")

	// pause the scheduler so we can unbalance the jobs without it trying
	// to rectify the situation
	s.Pause()

	// move host2's job to host3
	var job *host.ActiveJob
	jobs, err := host2.ListJobs()
	for _, j := range jobs {
		if j.Status == host.StatusStarting {
			job = &j
			break
		}
	}
	if job == nil {
		c.Fatal("could not find host2's omni job")
	}
	newJob := job.Job.Dup()
	newJob.ID = cluster.GenerateJobID(host3.ID(), s.generateJobUUID())
	host3.AddJob(newJob)
	err = host2.StopJob(job.Job.ID)
	c.Assert(err, IsNil)

	// resume the scheduler and check it moves the job back to host2
	s.Resume()
	s.waitJobStart()
	s.waitJobStart()
	assertJobs(hostJobs{
		host1: {
			{Type: "web", State: JobStateStarting},
			{Type: "omni", State: JobStateStarting},
		},
		host2: {
			{Type: "omni", State: JobStateStopped},
			{Type: "omni", State: JobStateStarting},
		},
		host3: {
			{Type: "omni", State: JobStateStopped},
			{Type: "omni", State: JobStateStarting},
			{Type: "omni", State: JobStateStopped},
		},
	})

	c.Logf("Remove one of the hosts. Ensure the cluster recovers correctly (hosts=%v)", hosts)
	host3.Healthy = false
	fakeCluster.SetHosts(hosts)
	s.waitFormationSync()
	s.waitRectify()
	assertJobs(hostJobs{
		host1: {
			{Type: "web", State: JobStateStarting},
			{Type: "omni", State: JobStateStarting},
		},
		host2: {
			{Type: "omni", State: JobStateStopped},
			{Type: "omni", State: JobStateStarting},
		},
		host3: {
			{Type: "omni", State: JobStateStopped},
			{Type: "omni", State: JobStateStopped},
			{Type: "omni", State: JobStateStopped},
		},
	})

	c.Logf("Remove another host. Ensure the cluster recovers correctly (hosts=%v)", hosts)
	host1.(*FakeHostClient).Healthy = false
	fakeCluster.RemoveHost(host1.ID())
	s.waitFormationSync()
	s.waitJobStart()
	assertJobs(hostJobs{
		host1: {
			{Type: "web", State: JobStateStopped},
			{Type: "omni", State: JobStateStopped},
		},
		host2: {
			{Type: "omni", State: JobStateStopped},
			{Type: "omni", State: JobStateStarting},
			{Type: "web", State: JobStateStarting},
		},
		host3: {
			{Type: "omni", State: JobStateStopped},
			{Type: "omni", State: JobStateStopped},
			{Type: "omni", State: JobStateStopped},
		},
	})
}

func (TestSuite) TestMultipleSchedulers(c *C) {
	// Set up cluster and both schedulers
	cluster := newTestCluster(nil)
	s1 := runTestScheduler(c, cluster, false)
	defer s1.Stop()
	s2 := runTestScheduler(c, cluster, false)
	defer s2.Stop()

	_, err := s1.waitDurationForEvent("handled job start event", 1*time.Second, nil)
	c.Assert(err, NotNil)
	_, err = s2.waitDurationForEvent("handled job start event", 1*time.Second, nil)
	c.Assert(err, NotNil)

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
	_, err = s2.waitDurationForEvent("handled job start event", 1*time.Second, nil)
	c.Assert(err, NotNil)
	_, err = s1.waitDurationForEvent("handled job start event", 1*time.Second, nil)
	c.Assert(err, NotNil)

	s2.discoverd.promote()
	s2.waitJobStart()
	s1.waitJobStart()
}

func (TestSuite) TestStopJob(c *C) {
	s := &Scheduler{putJobs: make(chan *ct.Job)}
	defer close(s.putJobs)
	go func() {
		for range s.putJobs {
		}
	}()
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
			jobs:       Jobs{"job1": &Job{ID: "job1", Formation: formation, Type: "web", State: JobStateRunning}},
			shouldStop: "job1",
		},
		{
			desc: "multiple running jobs, stops most recent",
			jobs: Jobs{
				"job1": &Job{ID: "job1", Formation: formation, Type: "web", State: JobStateRunning, StartedAt: recent.Add(-5 * time.Minute)},
				"job2": &Job{ID: "job2", Formation: formation, Type: "web", State: JobStateRunning, StartedAt: recent},
				"job3": &Job{ID: "job3", Formation: formation, Type: "web", State: JobStateRunning, StartedAt: recent.Add(-10 * time.Minute)},
			},
			shouldStop: "job2",
		},
		{
			desc: "one running and one stopped, stops running job",
			jobs: Jobs{
				"job1": &Job{ID: "job1", Formation: formation, Type: "web", State: JobStateRunning, StartedAt: recent.Add(-5 * time.Minute)},
				"job2": &Job{ID: "job2", Formation: formation, Type: "web", State: JobStateStopped, StartedAt: recent},
			},
			shouldStop: "job1",
		},
		{
			desc: "one running and one scheduled, stops scheduled job",
			jobs: Jobs{
				"job1": &Job{ID: "job1", Formation: formation, Type: "web", State: JobStatePending, StartedAt: recent.Add(-5 * time.Minute), restartTimer: time.NewTimer(0)},
				"job2": &Job{ID: "job2", Formation: formation, Type: "web", State: JobStateRunning, StartedAt: recent},
			},
			shouldStop: "job1",
		},
		{
			desc: "one running and one new, stops new job",
			jobs: Jobs{
				"job1": &Job{ID: "job1", Formation: formation, Type: "web", State: JobStatePending, StartedAt: recent.Add(-5 * time.Minute)},
				"job2": &Job{ID: "job2", Formation: formation, Type: "web", State: JobStateRunning, StartedAt: recent},
			},
			shouldStop: "job1",
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
	}
}

func (TestSuite) TestJobPlacementTags(c *C) {
	// create a scheduler with tagged hosts
	s := &Scheduler{
		isLeader: typeconv.BoolPtr(true),
		jobs:     make(Jobs),
		hosts: map[string]*Host{
			"host1": {ID: "host1", Tags: map[string]string{"disk": "mag", "cpu": "fast"}},
			"host2": {ID: "host2", Tags: map[string]string{"disk": "ssd", "cpu": "slow"}},
			"host3": {ID: "host3", Tags: map[string]string{"disk": "ssd", "cpu": "fast"}},
		},
		logger: log15.New(),
	}

	// use a formation with tagged process types
	formation := NewFormation(&ct.ExpandedFormation{
		App: &ct.App{ID: "app"},
		Release: &ct.Release{ID: "release", Processes: map[string]ct.ProcessType{
			"web":    {},
			"db":     {},
			"worker": {},
			"clock":  {},
		}},
		Artifacts: []*ct.Artifact{{}},
		Tags: map[string]map[string]string{
			"web":    nil,
			"db":     {"disk": "ssd"},
			"worker": {"cpu": "fast"},
			"clock":  {"disk": "ssd", "cpu": "slow"},
		},
	})

	// continually place jobs, and check they get placed in a round-robin
	// fashion on the hosts matching the type's tags
	type test struct {
		typ   string
		count int
		hosts map[string]int
	}
	for _, t := range []*test{
		// web go on all hosts
		{
			typ:   "web",
			count: 6,
			hosts: map[string]int{
				"host1": 2,
				"host2": 2,
				"host3": 2,
			},
		},

		// db go on hosts 2 and 3
		{
			typ:   "db",
			count: 4,
			hosts: map[string]int{
				"host2": 2,
				"host3": 2,
			},
		},

		// worker go on hosts 1 and 3
		{
			typ:   "worker",
			count: 4,
			hosts: map[string]int{
				"host1": 2,
				"host3": 2,
			},
		},

		// clock go on host 2
		{
			typ:   "clock",
			count: 4,
			hosts: map[string]int{
				"host2": 4,
			},
		},
	} {
		hosts := make(map[string]int, 3)
		for i := 0; i < t.count; i++ {
			job := s.jobs.Add(&Job{ID: fmt.Sprintf("job-%s-%d", t.typ, i), Formation: formation, Type: t.typ, State: JobStatePending})
			req := &PlacementRequest{Job: job, Err: make(chan error, 1)}
			s.HandlePlacementRequest(req)
			c.Assert(<-req.Err, IsNil, Commentf("placing %s job %d", t.typ, i))
			hosts[req.Host.ID]++
		}
		c.Assert(hosts, DeepEquals, t.hosts, Commentf("placing %s jobs", t.typ))
	}
}

func (TestSuite) TestScaleCriticalApp(c *C) {
	s := runTestScheduler(c, nil, true)
	defer s.Stop()
	s.waitJobStart()

	// scale a critical app up
	app := &ct.App{ID: "critical-app", Meta: map[string]string{"flynn-system-critical": "true"}}
	artifact := &ct.Artifact{ID: random.UUID()}
	processes := map[string]int{"critical": 1}
	release := NewRelease("critical-release-1", artifact, processes)
	s.CreateApp(app)
	s.CreateArtifact(artifact)
	s.CreateRelease(app.ID, release)
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: processes})
	s.waitJobStart()

	// check we can't scale it down
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: nil})
	_, err := s.waitForEvent("refusing to scale down critical app", nil)
	s.c.Assert(err, IsNil)
	s.waitFormationChange()

	// scale up another formation
	newRelease := NewRelease("critical-release-2", artifact, processes)
	s.CreateRelease(app.ID, newRelease)
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: newRelease.ID, Processes: processes})
	s.waitJobStart()

	// check we can now scale the original down
	s.PutFormation(&ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: nil})
	s.waitJobStop()
}

func (TestSuite) TestFindJobToStop(c *C) {
	s := &Scheduler{
		isLeader: typeconv.BoolPtr(true),
		jobs:     make(Jobs),
		logger:   log15.New(),
	}

	// populate s.jobs with jobs in various states, oldest first
	formation := &Formation{}
	typ := "web"
	start := time.Now()
	startedAt := func(i int) time.Time {
		return start.Add(time.Duration(int64(i)) * time.Second)
	}
	for i, state := range []JobState{
		JobStatePending,
		JobStateStarting,
		JobStateRunning,
		JobStatePending,
		JobStateStarting,
		JobStateRunning,
		JobStateStopping,
		JobStateStopped,
		JobStatePending,
		JobStateStarting,
		JobStateRunning,
	} {
		id := fmt.Sprintf("job%d", i)
		s.jobs[id] = &Job{
			ID:        id,
			Formation: formation,
			Type:      typ,
			StartedAt: startedAt(i),
			State:     state,
		}
	}

	nextJob := func() *Job {
		job, err := s.findJobToStop(formation, typ)
		c.Assert(err, IsNil)
		delete(s.jobs, job.ID)
		return job
	}

	// expect the three pending jobs first
	c.Assert(nextJob().State, Equals, JobStatePending)
	c.Assert(nextJob().State, Equals, JobStatePending)
	c.Assert(nextJob().State, Equals, JobStatePending)

	// then the starting jobs, newest first
	c.Assert(nextJob().ID, Equals, "job9")
	c.Assert(nextJob().ID, Equals, "job4")
	c.Assert(nextJob().ID, Equals, "job1")

	// then the running jobs, newest first
	c.Assert(nextJob().ID, Equals, "job10")
	c.Assert(nextJob().ID, Equals, "job5")
	c.Assert(nextJob().ID, Equals, "job2")
}

type failingHostClient struct {
	*FakeHostClient

	addJob chan struct{}
}

func newFailingHostClient() *failingHostClient {
	return &failingHostClient{
		FakeHostClient: NewFakeHostClient("failing_host", false),
		addJob:         make(chan struct{}),
	}
}

func (c *failingHostClient) AddJob(job *host.Job) error {
	go func() {
		<-c.addJob
		c.FakeHostClient.AddJob(job)
	}()
	return errors.New("fail")
}

func (TestSuite) TestFailingAddJob(c *C) {
	// create a cluster with a single host which fails when adding jobs
	failingHost := newFailingHostClient()
	cluster := NewFakeCluster()
	cluster.AddHost(failingHost)

	// start the scheduler
	s := newTestScheduler(c, cluster, true, map[string]int{"web": 0})
	go s.Run()
	defer s.Stop()
	s.waitForHost()

	// scale up the formation, wait for a job placement failure
	s.PutFormation(&ct.Formation{AppID: testAppID, ReleaseID: testReleaseID, Processes: map[string]int{"web": 1}})
	addJobErr := "error adding job to the cluster"
	s.waitForError(addJobErr)

	// add an ok host, wait for the job to be scheduled on it
	okHost := NewFakeHostClient("ok_host", false)
	events := make(chan *host.Event)
	stream, err := okHost.StreamEvents("", events)
	c.Assert(err, IsNil)
	defer stream.Close()
	cluster.AddHost(okHost)
	_, err = s.waitForEvent("handled host event", &addJobErr)
	s.c.Assert(err, IsNil)
	select {
	case <-events:
	case <-time.After(10 * time.Second):
		c.Fatal("timed out waiting for job to be scheduled on ok host")
	}

	// trigger the failing host to add its job, check it gets killed
	events = make(chan *host.Event)
	stream, err = failingHost.StreamEvents("", events)
	c.Assert(err, IsNil)
	defer stream.Close()
	close(failingHost.addJob)
loop:
	for {
		select {
		case event := <-events:
			if event.Event == host.JobEventStop {
				break loop
			}
		case <-time.After(10 * time.Second):
			c.Fatal("timed out waiting for job to be stopped on failing host")
		}
	}
}
