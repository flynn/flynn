package main

import (
	"net"
	"strings"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
)

type SchedulerSuite struct {
	Helper
}

const scaleTimeout = 60 * time.Second

var _ = c.Suite(&SchedulerSuite{})

func (s *SchedulerSuite) checkJobState(t *c.C, appID, jobID, state string) {
	job, err := s.controllerClient(t).GetJob(appID, jobID)
	t.Assert(err, c.IsNil)
	t.Assert(job.State, c.Equals, state)
}

func jobEventsEqual(expected, actual ct.JobEvents) bool {
	for typ, events := range expected {
		diff, ok := actual[typ]
		if !ok {
			return false
		}
		for state, count := range events {
			if diff[state] != count {
				return false
			}
		}
	}
	return true
}

func (s *SchedulerSuite) TestScale(t *c.C) {
	app, release := s.createApp(t)

	formation := &ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: make(map[string]int),
	}

	current := make(map[string]int)
	updates := []map[string]int{
		{"printer": 2},
		{"printer": 3, "crasher": 1},
		{"printer": 1},
	}

	watcher, err := s.controllerClient(t).WatchJobEvents(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	for _, procs := range updates {
		debugf(t, "scaling formation to %v", procs)
		formation.Processes = procs
		expected := s.controllerClient(t).ExpectedScalingEvents(current, procs, release.Processes, 1)
		t.Assert(s.controllerClient(t).PutFormation(formation), c.IsNil)

		err = watcher.WaitFor(expected, scaleTimeout, nil)
		t.Assert(err, c.IsNil)

		current = procs
	}
}

func (s *SchedulerSuite) TestControllerRestart(t *c.C) {
	// get the current controller details
	app, err := s.controllerClient(t).GetApp("controller")
	t.Assert(err, c.IsNil)
	release, err := s.controllerClient(t).GetAppRelease("controller")
	t.Assert(err, c.IsNil)
	formation, err := s.controllerClient(t).GetFormation(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	list, err := s.controllerClient(t).JobList("controller")
	t.Assert(err, c.IsNil)
	var jobs []*ct.Job
	for _, job := range list {
		if job.Type == "web" && job.State == "up" {
			jobs = append(jobs, job)
		}
	}
	t.Assert(jobs, c.HasLen, 2)
	hostID, jobID, _ := cluster.ParseJobID(jobs[0].ID)
	t.Assert(hostID, c.Not(c.Equals), "")
	t.Assert(jobID, c.Not(c.Equals), "")
	debugf(t, "current controller app[%s] host[%s] job[%s]", app.ID, hostID, jobID)

	// start another controller and wait for it to come up
	watcher, err := s.controllerClient(t).WatchJobEvents("controller", release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()
	debug(t, "scaling the controller up")
	formation.Processes["web"]++
	t.Assert(s.controllerClient(t).PutFormation(formation), c.IsNil)
	err = watcher.WaitFor(ct.JobEvents{"web": {"up": 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)

	// kill the first controller and check the scheduler brings it back online
	cc := cluster.NewClientWithServices(s.discoverdClient(t).Service)
	hc, err := cc.Host(hostID)
	t.Assert(err, c.IsNil)
	debug(t, "stopping job ", jobID)
	t.Assert(hc.StopJob(jobID), c.IsNil)
	err = watcher.WaitFor(ct.JobEvents{"web": {"down": 1, "up": 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)

	// scale back down
	debug(t, "scaling the controller down")
	formation.Processes["web"]--
	t.Assert(s.controllerClient(t).PutFormation(formation), c.IsNil)
	err = watcher.WaitFor(ct.JobEvents{"web": {"down": 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)

	// unset the suite's client so other tests use a new client
	s.controller = nil
}

func (s *SchedulerSuite) TestJobMeta(t *c.C) {
	app, release := s.createApp(t)

	watcher, err := s.controllerClient(t).WatchJobEvents(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	// start 1 one-off job
	_, err = s.controllerClient(t).RunJobDetached(app.ID, &ct.NewJob{
		ReleaseID: release.ID,
		Cmd:       []string{"sh", "-c", "while true; do echo one-off-job; sleep 1; done"},
		Meta: map[string]string{
			"foo": "baz",
		},
	})
	t.Assert(err, c.IsNil)
	err = watcher.WaitFor(ct.JobEvents{"": {"up": 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)

	list, err := s.controllerClient(t).JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 1)
	t.Assert(list[0].Meta, c.DeepEquals, map[string]string{
		"foo": "baz",
	})
}

func (s *SchedulerSuite) TestJobStatus(t *c.C) {
	app, release := s.createApp(t)

	watcher, err := s.controllerClient(t).WatchJobEvents(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	// start 2 formation processes and 1 one-off job
	t.Assert(s.controllerClient(t).PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"printer": 1, "crasher": 1},
	}), c.IsNil)
	_, err = s.controllerClient(t).RunJobDetached(app.ID, &ct.NewJob{
		ReleaseID: release.ID,
		Cmd:       []string{"sh", "-c", "while true; do echo one-off-job; sleep 1; done"},
	})
	t.Assert(err, c.IsNil)
	err = watcher.WaitFor(ct.JobEvents{"printer": {"up": 1}, "crasher": {"up": 1}, "": {"up": 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)

	list, err := s.controllerClient(t).JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 3)
	jobs := make(map[string]*ct.Job, len(list))
	for _, job := range list {
		debug(t, job.Type, "job started with ID ", job.ID)
		jobs[job.Type] = job
	}

	// Check jobs are marked as up once started
	t.Assert(jobs["printer"].State, c.Equals, "up")
	t.Assert(jobs["crasher"].State, c.Equals, "up")
	t.Assert(jobs[""].State, c.Equals, "up")

	// Check that when a formation's job is removed, it is marked as down and a new one is scheduled
	job := jobs["printer"]
	s.stopJob(t, job.ID)
	err = watcher.WaitFor(ct.JobEvents{"printer": {"down": 1, "up": 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)
	s.checkJobState(t, app.ID, job.ID, "down")
	list, err = s.controllerClient(t).JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 4)

	// Check that when a one-off job is removed, it is marked as down but a new one is not scheduled
	job = jobs[""]
	s.stopJob(t, job.ID)
	err = watcher.WaitFor(ct.JobEvents{"": {"down": 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)
	s.checkJobState(t, app.ID, job.ID, "down")
	list, err = s.controllerClient(t).JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 4)

	// Check that when a job errors, it is marked as crashed and a new one is started
	job = jobs["crasher"]
	s.stopJob(t, job.ID)
	err = watcher.WaitFor(ct.JobEvents{"crasher": {"down": 1, "up": 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)
	s.checkJobState(t, app.ID, job.ID, "crashed")
	list, err = s.controllerClient(t).JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 5)
}

func (s *SchedulerSuite) TestOmniJobs(t *c.C) {
	if testCluster == nil {
		t.Skip("cannot boot new hosts")
	}

	app, release := s.createApp(t)

	watcher, err := s.controllerClient(t).WatchJobEvents(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	formation := &ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: make(map[string]int),
	}

	current := make(map[string]int)
	updates := []map[string]int{
		{"printer": 2},
		{"printer": 3, "omni": 2},
		{"printer": 1, "omni": 1},
	}

	for _, procs := range updates {
		debugf(t, "scaling formation to %v", procs)
		formation.Processes = procs
		t.Assert(s.controllerClient(t).PutFormation(formation), c.IsNil)

		expected := s.controllerClient(t).ExpectedScalingEvents(current, procs, release.Processes, testCluster.Size())
		err = watcher.WaitFor(expected, scaleTimeout, nil)
		t.Assert(err, c.IsNil)

		current = procs
	}

	// Check that new hosts get omni jobs
	newHosts := s.addHosts(t, 2, false)
	defer s.removeHosts(t, newHosts)
	err = watcher.WaitFor(ct.JobEvents{"omni": {"up": 2}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)
}

func (s *SchedulerSuite) TestJobRestartBackoffPolicy(t *c.C) {
	if testCluster == nil {
		t.Skip("cannot determine scheduler backoff period")
	}
	backoffPeriod := testCluster.BackoffPeriod()
	startTimeout := 20 * time.Second
	debugf(t, "job restart backoff period: %s", backoffPeriod)

	app, release := s.createApp(t)

	watcher, err := s.controllerClient(t).WatchJobEvents(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	t.Assert(s.controllerClient(t).PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"printer": 1},
	}), c.IsNil)
	var id string
	var assignId = func(e *ct.JobEvent) error {
		id = e.JobID
		return nil
	}
	err = watcher.WaitFor(ct.JobEvents{"printer": {"up": 1}}, scaleTimeout, assignId)

	// First restart: scheduled immediately
	s.stopJob(t, id)
	err = watcher.WaitFor(ct.JobEvents{"printer": {"up": 1}}, startTimeout, assignId)
	t.Assert(err, c.IsNil)

	// Second restart after 1 * backoffPeriod
	start := time.Now()
	s.stopJob(t, id)
	err = watcher.WaitFor(ct.JobEvents{"printer": {"up": 1}}, backoffPeriod+startTimeout, assignId)
	t.Assert(err, c.IsNil)
	t.Assert(time.Now().Sub(start) > backoffPeriod, c.Equals, true)

	// Third restart after 2 * backoffPeriod
	start = time.Now()
	s.stopJob(t, id)
	err = watcher.WaitFor(ct.JobEvents{"printer": {"up": 1}}, 2*backoffPeriod+startTimeout, assignId)
	t.Assert(err, c.IsNil)
	t.Assert(time.Now().Sub(start) > 2*backoffPeriod, c.Equals, true)

	// After backoffPeriod has elapsed: scheduled immediately
	time.Sleep(backoffPeriod)
	s.stopJob(t, id)
	err = watcher.WaitFor(ct.JobEvents{"printer": {"up": 1}}, startTimeout, assignId)
	t.Assert(err, c.IsNil)
}

func (s *SchedulerSuite) TestTCPApp(t *c.C) {
	app, _ := s.createApp(t)

	t.Assert(flynn(t, "/", "-a", app.Name, "scale", "echoer=1"), Succeeds)

	newRoute := flynn(t, "/", "-a", app.Name, "route", "add", "tcp", "-s", "echo-service")
	t.Assert(newRoute, Succeeds)
	t.Assert(newRoute.Output, Matches, `.+ on port \d+`)
	str := strings.Split(strings.TrimSpace(string(newRoute.Output)), " ")
	port := str[len(str)-1]

	// use Attempts to give the processes time to start
	if err := Attempts.Run(func() error {
		servAddr := routerIP + ":" + port
		conn, err := net.Dial("tcp", servAddr)
		if err != nil {
			return err
		}
		defer conn.Close()
		msg := []byte("hello there!\n")
		_, err = conn.Write(msg)
		if err != nil {
			return err
		}
		reply := make([]byte, len(msg))
		_, err = conn.Read(reply)
		if err != nil {
			return err
		}
		t.Assert(reply, c.DeepEquals, msg)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func (s *SchedulerSuite) TestDeployController(t *c.C) {
	if testCluster == nil {
		t.Skip("cannot determine test cluster size")
	}

	// get the current controller release
	client := s.controllerClient(t)
	app, err := client.GetApp("controller")
	t.Assert(err, c.IsNil)
	release, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)

	// create a controller deployment
	release.ID = ""
	t.Assert(client.CreateRelease(release), c.IsNil)
	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)

	events := make(chan *ct.DeploymentEvent)
	eventStream, err := client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer eventStream.Close()

	// wait for the deploy to complete (this doesn't wait for specific events
	// due to the fact that when the deployer deploys itself, some events will
	// not get sent)
loop:
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("unexpected close of deployment event stream")
			}
			debugf(t, "got deployment event: %s %s", e.JobType, e.JobState)
			switch e.Status {
			case "complete":
				break loop
			case "failed":
				t.Fatal("the deployment failed")
			}
		case <-time.After(60 * time.Second):
			t.Fatal("timed out waiting for the deploy to complete")
		}
	}

	// check the correct controller jobs are running
	hosts, err := s.clusterClient(t).Hosts()
	t.Assert(err, c.IsNil)
	actual := make(map[string]map[string]int)
	for _, host := range hosts {
		jobs, err := host.ListJobs()
		t.Assert(err, c.IsNil)
		for _, job := range jobs {
			appID := job.Job.Metadata["flynn-controller.app"]
			if appID != app.ID {
				continue
			}
			releaseID := job.Job.Metadata["flynn-controller.release"]
			if _, ok := actual[releaseID]; !ok {
				actual[releaseID] = make(map[string]int)
			}
			typ := job.Job.Metadata["flynn-controller.type"]
			actual[releaseID][typ]++
		}
	}
	expected := map[string]map[string]int{release.ID: {
		"web":       2,
		"worker":    2,
		"scheduler": testCluster.Size(),
	}}
	t.Assert(actual, c.DeepEquals, expected)
}
