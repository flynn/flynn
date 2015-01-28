package main

import (
	"fmt"
	"net/http"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
)

type DeployerSuite struct {
	Helper
}

var _ = c.Suite(&DeployerSuite{})

func (s *DeployerSuite) createDeployment(t *c.C, app *ct.App, strategy string) *ct.Deployment {
	app.Strategy = strategy
	s.controllerClient(t).UpdateApp(app)
	release, err := s.controllerClient(t).GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)

	// create a new release for the deployment
	release.ID = ""
	t.Assert(s.controllerClient(t).CreateRelease(release), c.IsNil)

	deployment, err := s.controllerClient(t).CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	return deployment
}

func (s *DeployerSuite) createFormation(t *c.C, app *ct.App, processes map[string]int) *ct.Release {
	release, err := s.controllerClient(t).GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)

	jobStream := make(chan *ct.JobEvent)
	scale, err := s.controllerClient(t).StreamJobEvents(app.Name, 0, jobStream)
	t.Assert(err, c.IsNil)
	defer scale.Close()

	t.Assert(s.controllerClient(t).PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: processes,
	}), c.IsNil)

	waitEvents := jobEvents{}
	for proctype, i := range processes {
		if proctype == "failer" {
			continue
		}
		waitEvents[proctype] = map[string]int{"up": i}
	}
	waitForJobEvents(t, scale, jobStream, waitEvents)
	return release
}

func waitForDeploymentEvents(t *c.C, stream chan *ct.DeploymentEvent, expected []*ct.DeploymentEvent) {
	// wait for an event with no release to mark the end of the deployment,
	// collecting events along the way
	events := []*ct.DeploymentEvent{}
loop:
	for {
		select {
		case e := <-stream:
			events = append(events, e)
			if e.Status == "complete" || e.Status == "failed" {
				break loop
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for deployment event")
		}
	}
	compare := func(t *c.C, i *ct.DeploymentEvent, j *ct.DeploymentEvent) {
		debug(t, "Comparing", i, j)
		t.Assert(i.ReleaseID, c.Equals, j.ReleaseID)
		t.Assert(i.JobType, c.Equals, j.JobType)
		t.Assert(i.JobState, c.Equals, j.JobState)
		t.Assert(i.Status, c.Equals, j.Status)
	}

	for i, e := range expected {
		compare(t, events[i], e)
	}
}

func (s *DeployerSuite) TestOneByOneStrategy(t *c.C) {
	app, _ := s.createApp(t)
	s.createFormation(t, app, map[string]int{"printer": 2})
	deployment := s.createDeployment(t, app, "one-by-one")
	events := make(chan *ct.DeploymentEvent)
	stream, err := s.controllerClient(t).StreamDeployment(deployment.ID, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	releaseID := deployment.NewReleaseID
	oldReleaseID := deployment.OldReleaseID

	expected := []*ct.DeploymentEvent{
		{ReleaseID: releaseID, JobType: "printer", JobState: "starting", Status: "running"},
		{ReleaseID: releaseID, JobType: "printer", JobState: "up", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "stopping", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "down", Status: "running"},
		{ReleaseID: releaseID, JobType: "printer", JobState: "starting", Status: "running"},
		{ReleaseID: releaseID, JobType: "printer", JobState: "up", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "stopping", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "down", Status: "running"},
		{ReleaseID: releaseID, JobType: "", JobState: "", Status: "complete"},
	}
	waitForDeploymentEvents(t, events, expected)
}

func (s *DeployerSuite) TestAllAtOnceStrategy(t *c.C) {
	app, _ := s.createApp(t)
	s.createFormation(t, app, map[string]int{"printer": 2})
	deployment := s.createDeployment(t, app, "all-at-once")
	events := make(chan *ct.DeploymentEvent)
	stream, err := s.controllerClient(t).StreamDeployment(deployment.ID, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	releaseID := deployment.NewReleaseID
	oldReleaseID := deployment.OldReleaseID

	expected := []*ct.DeploymentEvent{
		{ReleaseID: releaseID, JobType: "printer", JobState: "starting", Status: "running"},
		{ReleaseID: releaseID, JobType: "printer", JobState: "starting", Status: "running"},
		{ReleaseID: releaseID, JobType: "printer", JobState: "up", Status: "running"},
		{ReleaseID: releaseID, JobType: "printer", JobState: "up", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "stopping", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "stopping", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "down", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "down", Status: "running"},
		{ReleaseID: releaseID, JobType: "", JobState: "", Status: "complete"},
	}
	waitForDeploymentEvents(t, events, expected)
}

func (s *DeployerSuite) TestRollback(t *c.C) {
	app, _ := s.createApp(t)
	s.createFormation(t, app, map[string]int{"failer": 2})
	deployment := s.createDeployment(t, app, "all-at-once")
	events := make(chan *ct.DeploymentEvent)
	stream, err := s.controllerClient(t).StreamDeployment(deployment.ID, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	releaseID := deployment.NewReleaseID
	oldReleaseID := deployment.OldReleaseID

	expected := []*ct.DeploymentEvent{
		{ReleaseID: releaseID, JobType: "failer", JobState: "starting", Status: "running"},
		{ReleaseID: releaseID, JobType: "failer", JobState: "starting", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "failer", JobState: "crashed", Status: "running"},
		{ReleaseID: releaseID, JobType: "", JobState: "", Status: "failed"},
	}
	waitForDeploymentEvents(t, events, expected)

	// check that we're running the old release
	rel, err := s.controllerClient(t).GetAppRelease(deployment.AppID)
	t.Assert(err, c.IsNil)
	t.Assert(rel.ID, c.Equals, oldReleaseID)

	// check that the old formation is the same and there's no new formation
	f, err := s.controllerClient(t).GetFormation(deployment.AppID, oldReleaseID)
	t.Assert(err, c.IsNil)
	t.Assert(f.Processes, c.DeepEquals, map[string]int{"failer": 2})
	_, err = s.controllerClient(t).GetFormation(deployment.AppID, releaseID)
	t.Assert(err, c.NotNil)
}

func (s *DeployerSuite) TestNoDowntime(t *c.C) {
	app, _ := s.createApp(t)
	route := "ping-transitioning.dev"
	s.createFormation(t, app, map[string]int{"ping": 1})
	t.Assert(flynn(t, "/", "-a", app.Name, "route", "add", "http", "-s", "ping-service", route), Succeeds)
	_, err := s.discoverdClient(t).Instances("ping-service", 10*time.Second)
	t.Assert(err, c.IsNil)

	done := make(chan bool)
	fail := make(chan error)

	go func() {
	outer:
		for {
			select {
			case <-done:
				close(fail)
				break outer
			default:
				client := &http.Client{}
				req, err := http.NewRequest("GET", "http://"+routerIP, nil)
				if err != nil {
					fail <- err
					break outer
				}
				req.Host = route
				res, err := client.Do(req)
				if err != nil {
					fail <- err
					break outer
				}
				res.Body.Close()
				if res.StatusCode != 200 {
					fail <- fmt.Errorf("Expected res.StatusCode to equal 200, got %s", res.StatusCode)
					break outer
				}
			}
		}
	}()

	deployment := s.createDeployment(t, app, "all-at-once")
	events := make(chan *ct.DeploymentEvent)
	stream, err := s.controllerClient(t).StreamDeployment(deployment.ID, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	releaseID := deployment.NewReleaseID
	oldReleaseID := deployment.OldReleaseID

	expected := []*ct.DeploymentEvent{
		{ReleaseID: releaseID, JobType: "ping", JobState: "starting", Status: "running"},
		{ReleaseID: releaseID, JobType: "ping", JobState: "up", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "ping", JobState: "stopping", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "ping", JobState: "down", Status: "running"},
		{ReleaseID: releaseID, JobType: "", JobState: "", Status: "complete"},
	}

	waitForDeploymentEvents(t, events, expected)
	close(done)
	if err := <-fail; err != nil {
		t.Error(err)
	}
}
