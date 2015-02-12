package main

import (
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
)

type DeployerSuite struct {
	Helper
}

var _ = c.Suite(&DeployerSuite{})

func (s *DeployerSuite) createDeployment(t *c.C, process, strategy string) *ct.Deployment {
	app, release := s.createApp(t)
	app.Strategy = strategy
	s.controllerClient(t).UpdateApp(app)

	jobStream := make(chan *ct.JobEvent)
	scale, err := s.controllerClient(t).StreamJobEvents(app.Name, 0, jobStream)
	t.Assert(err, c.IsNil)
	defer scale.Close()

	t.Assert(s.controllerClient(t).PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{process: 2},
	}), c.IsNil)

	waitForJobEvents(t, scale, jobStream, jobEvents{process: {"up": 2}})

	// create a new release for the deployment
	release.ID = ""
	t.Assert(s.controllerClient(t).CreateRelease(release), c.IsNil)

	deployment, err := s.controllerClient(t).CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	return deployment
}

func waitForDeploymentEvents(t *c.C, stream chan *ct.DeploymentEvent, expected []*ct.DeploymentEvent) {
	debugf(t, "waiting for %d deployment events", len(expected))
	actual := make([]*ct.DeploymentEvent, 0, len(expected))
loop:
	for {
		select {
		case e := <-stream:
			debugf(t, "got deployment event: %s %s", e.JobType, e.JobState)
			actual = append(actual, e)
			if e.Status == "complete" || e.Status == "failed" {
				break loop
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for deployment event")
		}
	}
	compare := func(t *c.C, i *ct.DeploymentEvent, j *ct.DeploymentEvent) {
		t.Assert(i.ReleaseID, c.Equals, j.ReleaseID)
		t.Assert(i.JobType, c.Equals, j.JobType)
		t.Assert(i.JobState, c.Equals, j.JobState)
		t.Assert(i.Status, c.Equals, j.Status)
	}

	for i, e := range expected {
		compare(t, actual[i], e)
	}
}

func (s *DeployerSuite) TestOneByOneStrategy(t *c.C) {
	deployment := s.createDeployment(t, "printer", "one-by-one")
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
	deployment := s.createDeployment(t, "printer", "all-at-once")
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
	deployment := s.createDeployment(t, "crasher", "all-at-once")
	events := make(chan *ct.DeploymentEvent)
	stream, err := s.controllerClient(t).StreamDeployment(deployment.ID, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	releaseID := deployment.NewReleaseID
	oldReleaseID := deployment.OldReleaseID

	expected := []*ct.DeploymentEvent{
		{ReleaseID: releaseID, JobType: "crasher", JobState: "starting", Status: "running"},
		{ReleaseID: releaseID, JobType: "crasher", JobState: "starting", Status: "running"},
		{ReleaseID: releaseID, JobType: "crasher", JobState: "up", Status: "running"},
		{ReleaseID: releaseID, JobType: "crasher", JobState: "up", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "crasher", JobState: "stopping", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "crasher", JobState: "stopping", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "crasher", JobState: "crashed", Status: "running"},
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
	t.Assert(f.Processes, c.DeepEquals, map[string]int{"crasher": 2})
	_, err = s.controllerClient(t).GetFormation(deployment.AppID, releaseID)
	t.Assert(err, c.NotNil)
}
