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

func (s *DeployerSuite) createRelease(t *c.C, process, strategy string) (*ct.App, *ct.Release) {
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

	return app, release
}

func (s *DeployerSuite) createDeployment(t *c.C, process, strategy string) *ct.Deployment {
	app, release := s.createRelease(t, process, strategy)

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
			actual = append(actual, e)
			if e.Status == "complete" || e.Status == "failed" {
				debugf(t, "got deployment event: %s", e.Status)
				break loop
			}
			debugf(t, "got deployment event: %s %s", e.JobType, e.JobState)
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
	// create a running release
	app, release := s.createRelease(t, "printer", "all-at-once")

	// deploy a release which will fail to start
	client := s.controllerClient(t)
	release.ID = ""
	printer := release.Processes["printer"]
	printer.Cmd = []string{"this-is-gonna-fail"}
	release.Processes["printer"] = printer
	t.Assert(client.CreateRelease(release), c.IsNil)
	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)

	// check the deployment fails
	events := make(chan *ct.DeploymentEvent)
	stream, err := client.StreamDeployment(deployment.ID, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	expected := []*ct.DeploymentEvent{
		{ReleaseID: release.ID, JobType: "printer", JobState: "starting", Status: "running"},
		{ReleaseID: release.ID, JobType: "printer", JobState: "starting", Status: "running"},
		{ReleaseID: release.ID, JobType: "printer", JobState: "failed", Status: "running"},
		{ReleaseID: release.ID, JobType: "", JobState: "", Status: "failed"},
	}
	waitForDeploymentEvents(t, events, expected)

	// check that we're running the old release
	rel, err := client.GetAppRelease(deployment.AppID)
	t.Assert(err, c.IsNil)
	t.Assert(rel.ID, c.Equals, deployment.OldReleaseID)

	// check that the old formation is the same and there's no new formation
	f, err := client.GetFormation(deployment.AppID, deployment.OldReleaseID)
	t.Assert(err, c.IsNil)
	t.Assert(f.Processes, c.DeepEquals, map[string]int{"printer": 2})
	_, err = client.GetFormation(deployment.AppID, deployment.NewReleaseID)
	t.Assert(err, c.NotNil)
}
