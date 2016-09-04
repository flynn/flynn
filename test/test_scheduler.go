package main

import (
	"encoding/json"
	"net"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	logaggc "github.com/flynn/flynn/logaggregator/client"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/typeconv"
	c "github.com/flynn/go-check"
)

type SchedulerSuite struct {
	Helper
}

const scaleTimeout = 60 * time.Second

var _ = c.Suite(&SchedulerSuite{})

func (s *SchedulerSuite) checkJobState(t *c.C, appID, jobID string, state ct.JobState) {
	job, err := s.controllerClient(t).GetJob(appID, jobID)
	t.Assert(err, c.IsNil)
	t.Assert(job.State, c.Equals, state)
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

func (s *SchedulerSuite) TestScaleTags(t *c.C) {
	// ensure we have more than 1 host to test with
	hosts, err := s.clusterClient(t).Hosts()
	t.Assert(err, c.IsNil)
	if len(hosts) <= 1 {
		t.Skip("not enough hosts to test tagged based scheduling")
	}

	// stream the scheduler leader log so we can synchronize tag changes
	leader, err := s.discoverdClient(t).Service("controller-scheduler").Leader()
	t.Assert(err, c.IsNil)
	client := s.controllerClient(t)
	res, err := client.GetAppLog("controller", &ct.LogOpts{
		Follow:      true,
		JobID:       leader.Meta["FLYNN_JOB_ID"],
		ProcessType: typeconv.StringPtr("scheduler"),
		Lines:       typeconv.IntPtr(0),
	})
	t.Assert(err, c.IsNil)
	defer res.Close()
	tagChange := make(chan struct{})
	go func() {
		dec := json.NewDecoder(res)
		for {
			var msg logaggc.Message
			if err := dec.Decode(&msg); err != nil {
				return
			}
			if strings.Contains(msg.Msg, "host tags changed") {
				tagChange <- struct{}{}
			}
		}
	}()
	waitSchedulerTagChange := func() {
		select {
		case <-tagChange:
			return
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for scheduler leader to see tag change")
		}
	}

	// watch service events so we can wait for tag changes
	events := make(chan *discoverd.Event)
	stream, err := s.discoverdClient(t).Service("flynn-host").Watch(events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	waitServiceEvent := func(kind discoverd.EventKind) *discoverd.Event {
		for {
			select {
			case event, ok := <-events:
				if !ok {
					t.Fatalf("service event stream closed unexpectedly: %s", stream.Err())
				}
				if event.Kind == kind {
					return event
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("timed out waiting for service %s event", kind)
			}
		}
	}

	// wait for the watch to be current before changing tags
	waitServiceEvent(discoverd.EventKindCurrent)

	updateTags := func(host *cluster.Host, tags map[string]string) {
		debugf(t, "setting host tags: %s => %v", host.ID(), tags)
		t.Assert(host.UpdateTags(tags), c.IsNil)
		event := waitServiceEvent(discoverd.EventKindUpdate)
		t.Assert(event.Instance.Meta["id"], c.Equals, host.ID())
		for key, val := range tags {
			t.Assert(event.Instance.Meta["tag:"+key], c.Equals, val)
		}
		waitSchedulerTagChange()
	}

	// create an app with a tagged process and watch job events
	app, release := s.createApp(t)
	formation := &ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Tags:      map[string]map[string]string{"printer": {"active": "true"}},
	}
	watcher, err := client.WatchJobEvents(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	// add tag to host 1
	host1 := hosts[0]
	updateTags(host1, map[string]string{"active": "true"})

	// start jobs
	debug(t, "scaling printer=2")
	formation.Processes = map[string]int{"printer": 2}
	t.Assert(client.PutFormation(formation), c.IsNil)
	t.Assert(watcher.WaitFor(ct.JobEvents{"printer": ct.JobUpEvents(2)}, scaleTimeout, nil), c.IsNil)

	assertHostJobCounts := func(expected map[string]int) {
		jobs, err := client.JobList(app.ID)
		t.Assert(err, c.IsNil)
		actual := make(map[string]int)
		for _, job := range jobs {
			if job.State == ct.JobStateUp {
				actual[job.HostID]++
			}
		}
		t.Assert(actual, c.DeepEquals, expected)
	}

	// check all jobs on host 1
	assertHostJobCounts(map[string]int{host1.ID(): 2})

	// add tag to host 2
	host2 := hosts[1]
	updateTags(host2, map[string]string{"active": "true"})

	// scale up
	debug(t, "scaling printer=4")
	formation.Processes["printer"] = 4
	t.Assert(client.PutFormation(formation), c.IsNil)
	t.Assert(watcher.WaitFor(ct.JobEvents{"printer": ct.JobUpEvents(2)}, scaleTimeout, nil), c.IsNil)

	// check jobs distributed across hosts 1 and 2
	assertHostJobCounts(map[string]int{host1.ID(): 2, host2.ID(): 2})

	// remove tag from host 2
	updateTags(host2, map[string]string{"active": ""})

	// check jobs are moved to host1
	jobEvents := ct.JobEvents{"printer": map[ct.JobState]int{
		ct.JobStateDown: 2,
		ct.JobStateUp:   2,
	}}
	t.Assert(watcher.WaitFor(jobEvents, scaleTimeout, nil), c.IsNil)
	assertHostJobCounts(map[string]int{host1.ID(): 4})

	// remove tag from host 1
	updateTags(host1, map[string]string{"active": ""})

	assertStateCounts := func(expected map[ct.JobState]int) {
		jobs, err := client.JobList(app.ID)
		t.Assert(err, c.IsNil)
		actual := make(map[ct.JobState]int)
		for _, job := range jobs {
			actual[job.State]++
		}
		t.Assert(actual, c.DeepEquals, expected)
	}

	// check 4 pending jobs, rest are stopped
	t.Assert(watcher.WaitFor(ct.JobEvents{"printer": ct.JobDownEvents(4)}, scaleTimeout, nil), c.IsNil)
	assertStateCounts(map[ct.JobState]int{ct.JobStatePending: 4, ct.JobStateDown: 6})

	// re-add tag to host 1
	updateTags(host1, map[string]string{"active": "true"})

	// check pending jobs are started on host 1
	t.Assert(watcher.WaitFor(ct.JobEvents{"printer": ct.JobUpEvents(4)}, scaleTimeout, nil), c.IsNil)
	assertHostJobCounts(map[string]int{host1.ID(): 4})
	assertStateCounts(map[ct.JobState]int{ct.JobStateUp: 4, ct.JobStateDown: 6})

	// add different tag to host 2
	updateTags(host2, map[string]string{"disk": "ssd"})

	// update formation tags, check jobs are moved to host 2
	debug(t, "updating formation tags to disk=ssd")
	formation.Tags["printer"] = map[string]string{"disk": "ssd"}
	t.Assert(client.PutFormation(formation), c.IsNil)
	jobEvents = ct.JobEvents{"printer": map[ct.JobState]int{
		ct.JobStateDown: 4,
		ct.JobStateUp:   4,
	}}
	t.Assert(watcher.WaitFor(jobEvents, scaleTimeout, nil), c.IsNil)
	assertHostJobCounts(map[string]int{host2.ID(): 4})
	assertStateCounts(map[ct.JobState]int{ct.JobStateUp: 4, ct.JobStateDown: 10})

	// scale down stops the jobs
	debug(t, "scaling printer=0")
	formation.Processes = nil
	t.Assert(client.PutFormation(formation), c.IsNil)
	t.Assert(watcher.WaitFor(ct.JobEvents{"printer": ct.JobDownEvents(4)}, scaleTimeout, nil), c.IsNil)
	assertStateCounts(map[ct.JobState]int{ct.JobStateDown: 14})
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
		if job.Type == "web" && job.State == ct.JobStateUp {
			jobs = append(jobs, job)
		}
	}
	t.Assert(jobs, c.HasLen, formation.Processes["web"])
	jobID := jobs[0].ID
	hostID, _ := cluster.ExtractHostID(jobID)
	t.Assert(hostID, c.Not(c.Equals), "")
	debugf(t, "current controller app[%s] host[%s] job[%s]", app.ID, hostID, jobID)

	// subscribe to service events, wait for current event
	events := make(chan *discoverd.Event)
	stream, err := s.discoverdClient(t).Service("controller").Watch(events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	type serviceEvents map[discoverd.EventKind]int
	wait := func(expected serviceEvents) {
		actual := make(serviceEvents)
	outer:
		for {
			select {
			case event := <-events:
				actual[event.Kind]++
				for kind, count := range expected {
					if actual[kind] != count {
						continue outer
					}
				}
				return
			case <-time.After(scaleTimeout):
				t.Fatal("timed out waiting for controller service event")
			}
		}
	}
	wait(serviceEvents{discoverd.EventKindCurrent: 1})

	// start another controller and wait for it to come up
	debug(t, "scaling the controller up")
	formation.Processes["web"]++
	t.Assert(s.controllerClient(t).PutFormation(formation), c.IsNil)
	wait(serviceEvents{discoverd.EventKindUp: 1})

	// kill the first controller and check the scheduler brings it back online
	cc := cluster.NewClientWithServices(s.discoverdClient(t).Service)
	hc, err := cc.Host(hostID)
	t.Assert(err, c.IsNil)
	debug(t, "stopping job ", jobID)
	t.Assert(hc.StopJob(jobID), c.IsNil)
	wait(serviceEvents{discoverd.EventKindUp: 1, discoverd.EventKindDown: 1})

	// scale back down
	debug(t, "scaling the controller down")
	formation.Processes["web"]--
	t.Assert(s.controllerClient(t).PutFormation(formation), c.IsNil)
	wait(serviceEvents{discoverd.EventKindDown: 1})

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
		Args:      []string{"sh", "-c", "while true; do echo one-off-job; sleep 1; done"},
		Meta: map[string]string{
			"foo": "baz",
		},
	})
	t.Assert(err, c.IsNil)
	err = watcher.WaitFor(ct.JobEvents{"": {ct.JobStateUp: 1}}, scaleTimeout, nil)
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
		Args:      []string{"sh", "-c", "while true; do echo one-off-job; sleep 1; done"},
	})
	t.Assert(err, c.IsNil)
	err = watcher.WaitFor(ct.JobEvents{"printer": {ct.JobStateUp: 1}, "crasher": {ct.JobStateUp: 1}, "": {ct.JobStateUp: 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)

	list, err := s.controllerClient(t).JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 3)
	jobs := make(map[string]*ct.Job, len(list))
	for _, job := range list {
		debugf(t, "%s job started with ID %s", job.Type, job.ID)
		jobs[job.Type] = job
	}

	// Check jobs are marked as up once started
	t.Assert(jobs["printer"].State, c.Equals, ct.JobStateUp)
	t.Assert(jobs["crasher"].State, c.Equals, ct.JobStateUp)
	t.Assert(jobs[""].State, c.Equals, ct.JobStateUp)

	// Check that when a formation's job is removed, it is marked as down and a new one is scheduled
	job := jobs["printer"]
	s.stopJob(t, job.ID)
	err = watcher.WaitFor(ct.JobEvents{"printer": {ct.JobStateDown: 1, ct.JobStateUp: 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)
	s.checkJobState(t, app.ID, job.ID, ct.JobStateDown)
	list, err = s.controllerClient(t).JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 4)

	// Check that when a one-off job is removed, it is marked as down but a new one is not scheduled
	job = jobs[""]
	s.stopJob(t, job.ID)
	err = watcher.WaitFor(ct.JobEvents{"": {ct.JobStateDown: 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)
	s.checkJobState(t, app.ID, job.ID, ct.JobStateDown)
	list, err = s.controllerClient(t).JobList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(list, c.HasLen, 4)

	// Check that when a job errors, it is marked as down and a new one is started
	job = jobs["crasher"]
	s.stopJob(t, job.ID)
	err = watcher.WaitFor(ct.JobEvents{"crasher": {ct.JobStateDown: 1, ct.JobStateUp: 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)
	s.checkJobState(t, app.ID, job.ID, ct.JobStateDown)
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
	newHosts := s.addHosts(t, 2, false, "router-api")
	defer s.removeHosts(t, newHosts, "router-api")
	err = watcher.WaitFor(ct.JobEvents{"omni": {ct.JobStateUp: 2}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)
}

func (s *SchedulerSuite) TestJobRestartBackoffPolicy(t *c.C) {
	startTimeout := 20 * time.Second

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
	var assignId = func(j *ct.Job) error {
		debugf(t, "got job event: %s %s", j.ID, j.State)
		id = j.ID
		return nil
	}
	err = watcher.WaitFor(ct.JobEvents{"printer": {ct.JobStateUp: 1}}, scaleTimeout, assignId)
	t.Assert(err, c.IsNil)

	waitForRestart := func(duration time.Duration) {
		start := time.Now()
		s.stopJob(t, id)
		debugf(t, "expecting new job to start in %s", duration)
		err = watcher.WaitFor(ct.JobEvents{"printer": {ct.JobStateUp: 1}}, duration+startTimeout, assignId)
		t.Assert(err, c.IsNil)
		actual := time.Now().Sub(start)
		if actual < duration {
			t.Fatalf("expected new job to start after %s but started after %s", duration, actual)
		}
	}

	waitForRestart(0)
	waitForRestart(0)
	waitForRestart(0)
	waitForRestart(0)
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

func (s *SchedulerSuite) TestRollbackController(t *c.C) {
	// get the current controller release
	client := s.controllerClient(t)
	app, err := client.GetApp("controller")
	t.Assert(err, c.IsNil)
	release, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)

	watcher, err := s.controllerClient(t).WatchJobEvents(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	// get the current controller formation
	formation, err := client.GetFormation(app.ID, release.ID)
	t.Assert(err, c.IsNil)

	currentReleaseID := release.ID

	// create a controller deployment that will fail
	release.ID = ""
	worker := release.Processes["worker"]
	worker.Args = []string{"/i/dont/exist"}
	release.Processes["worker"] = worker
	t.Assert(client.CreateRelease(release), c.IsNil)
	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)

	events := make(chan *ct.DeploymentEvent)
	eventStream, err := client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer eventStream.Close()

	// wait for the deploy to fail
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
				t.Fatal("the deployment succeeded when it should have failed")
			case "failed":
				break loop
			}
		case <-time.After(2 * time.Minute):
			t.Fatal("timed out waiting for the deploy to fail")
		}
	}

	// wait for jobs to come back up
	hosts, err := s.clusterClient(t).Hosts()
	expected := map[string]map[ct.JobState]int{
		"web":       {ct.JobStateUp: formation.Processes["web"]},
		"scheduler": {ct.JobStateUp: len(hosts)},
	}
	t.Assert(watcher.WaitFor(expected, scaleTimeout, nil), c.IsNil)

	// check the correct controller jobs are running
	t.Assert(err, c.IsNil)
	t.Assert(hosts, c.Not(c.HasLen), 0)
	actual := make(map[string]map[string]int)
	for _, h := range hosts {
		jobs, err := h.ListJobs()
		t.Assert(err, c.IsNil)
		for _, job := range jobs {
			if job.Status != host.StatusRunning {
				continue
			}
			appID := job.Job.Metadata["flynn-controller.app"]
			if appID != app.ID {
				continue
			}
			releaseID := job.Job.Metadata["flynn-controller.release"]
			if releaseID != currentReleaseID {
				continue
			}
			if _, ok := actual[releaseID]; !ok {
				actual[releaseID] = make(map[string]int)
			}
			typ := job.Job.Metadata["flynn-controller.type"]
			actual[releaseID][typ]++
		}
	}
	t.Assert(actual, c.DeepEquals, map[string]map[string]int{
		currentReleaseID: {
			"web":       formation.Processes["web"],
			"scheduler": formation.Processes["scheduler"] * len(hosts),
			"worker":    formation.Processes["worker"],
		},
	})
}

func (s *SchedulerSuite) TestDeployController(t *c.C) {
	// get the current controller release
	client := s.controllerClient(t)
	app, err := client.GetApp("controller")
	t.Assert(err, c.IsNil)
	release, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)

	// get the current controller formation
	formation, err := client.GetFormation(app.ID, release.ID)
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
		case <-time.After(time.Duration(app.DeployTimeout) * time.Second):
			t.Fatal("timed out waiting for the deploy to complete")
		}
	}

	// check the correct controller jobs are running
	hosts, err := s.clusterClient(t).Hosts()
	t.Assert(err, c.IsNil)
	t.Assert(hosts, c.Not(c.HasLen), 0)
	actual := make(map[string]map[string]int)
	for _, h := range hosts {
		jobs, err := h.ListJobs()
		t.Assert(err, c.IsNil)
		for _, job := range jobs {
			if job.Status != host.StatusRunning {
				continue
			}
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
		"web":       formation.Processes["web"],
		"worker":    formation.Processes["worker"],
		"scheduler": len(hosts),
	}}
	t.Assert(actual, c.DeepEquals, expected)
}
