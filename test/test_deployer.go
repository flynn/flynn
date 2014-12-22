package main

import (
	"fmt"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
	deployerc "github.com/flynn/flynn/deployer/client"
	"github.com/flynn/flynn/deployer/types"
)

type DeployerSuite struct {
	Helper
}

var _ = c.Suite(&DeployerSuite{})

func (s *DeployerSuite) TearDownSuite(t *c.C) {
	s.cleanup()
}

func (s *DeployerSuite) createDeployment(t *c.C, strategy string) *deployer.Deployment {
	app, release := s.createApp(t)

	scale, err := s.controllerClient(t).StreamJobEvents(app.Name, 0)
	t.Assert(err, c.IsNil)

	t.Assert(s.controllerClient(t).PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"printer": 2},
	}), c.IsNil)

	// create a new release for the deployment
	oldReleaseID := release.ID
	release.ID = ""
	t.Assert(s.controllerClient(t).CreateRelease(release), c.IsNil)

	waitForJobEvents(t, scale.Events, jobEvents{"printer": {"up": 2}})

	client, err := deployerc.New()
	t.Assert(err, c.IsNil)

	deployment := &deployer.Deployment{
		AppID:        app.ID,
		OldReleaseID: oldReleaseID,
		NewReleaseID: release.ID,
		Strategy:     strategy,
		Steps: map[string]deployer.Step{
			"before_deployment": {Cmd: []string{"a", "b", "c"}},
			"after_deployment":  {Cmd: []string{"d", "e", "f"}},
		},
	}
	t.Assert(client.CreateDeployment(deployment), c.IsNil)
	return deployment
}

func waitForDeploymentEvents(t *c.C, stream chan *deployer.DeploymentEvent, expected []*deployer.DeploymentEvent) {
	// wait for an event with no release to mark the end of the deployment,
	// collecting events along the way
	events := []*deployer.DeploymentEvent{}
loop:
	for {
		select {
		case e := <-stream:
			events = append(events, e)
			if e.ReleaseID == "" {
				break loop
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for deployment event")
		}
	}
	compare := func(t *c.C, i *deployer.DeploymentEvent, j *deployer.DeploymentEvent) {
		fmt.Println("Comparing", i, j)
		t.Assert(i.ReleaseID, c.Equals, j.ReleaseID)
		t.Assert(i.JobType, c.Equals, j.JobType)
		t.Assert(i.JobState, c.Equals, j.JobState)
	}

	for i, e := range expected {
		compare(t, events[i], e)
	}
}

func (s *DeployerSuite) TestOneByOneStrategy(t *c.C) {
	deployment := s.createDeployment(t, "one-by-one")
	releaseID := deployment.NewReleaseID
	oldReleaseID := deployment.OldReleaseID

	client, err := deployerc.New()
	t.Assert(err, c.IsNil)
	stream, err := client.StreamDeploymentEvents(deployment.ID, 0)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	expected := []*deployer.DeploymentEvent{
		{ReleaseID: releaseID, JobType: "printer", JobState: "starting"},
		{ReleaseID: releaseID, JobType: "printer", JobState: "up"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "stopping"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "down"},
		{ReleaseID: releaseID, JobType: "printer", JobState: "starting"},
		{ReleaseID: releaseID, JobType: "printer", JobState: "up"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "stopping"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "down"},
		{ReleaseID: "", JobType: "", JobState: ""},
	}
	waitForDeploymentEvents(t, stream.Events, expected)
}

func (s *DeployerSuite) TestAllAtOnceStrategy(t *c.C) {
	deployment := s.createDeployment(t, "all-at-once")
	releaseID := deployment.NewReleaseID
	oldReleaseID := deployment.OldReleaseID

	client, err := deployerc.New()
	t.Assert(err, c.IsNil)
	stream, err := client.StreamDeploymentEvents(deployment.ID, 0)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	expected := []*deployer.DeploymentEvent{
		{ReleaseID: releaseID, JobType: "printer", JobState: "starting"},
		{ReleaseID: releaseID, JobType: "printer", JobState: "up"},
		{ReleaseID: releaseID, JobType: "printer", JobState: "starting"},
		{ReleaseID: releaseID, JobType: "printer", JobState: "up"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "stopping"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "down"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "stopping"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "down"},
		{ReleaseID: "", JobType: "", JobState: ""},
	}
	waitForDeploymentEvents(t, stream.Events, expected)
}
