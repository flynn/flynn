package main

import (
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

func (s *DeployerSuite) TestDeployment(t *c.C) {
	app, release := s.createApp(t)
	t.Assert(s.controllerClient(t).PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"printer": 2},
	}), c.IsNil)

	// create a new release for the deployment
	oldReleaseID := release.ID
	release.ID = ""
	t.Assert(s.controllerClient(t).CreateRelease(release), c.IsNil)

	client, err := deployerc.New()
	t.Assert(err, c.IsNil)

	stream, err := client.StreamDeploymentEvents(app.ID, 0)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	deployment := &deployer.Deployment{
		AppID:        app.ID,
		OldReleaseID: oldReleaseID,
		NewReleaseID: release.ID,
		Strategy:     "one-by-one",
		Steps: map[string]deployer.Step{
			"before_deployment": {Cmd: []string{"a", "b", "c"}},
			"after_deployment":  {Cmd: []string{"d", "e", "f"}},
		},
	}
	t.Assert(client.CreateDeployment(deployment), c.IsNil)

	// wait for an event with no release to mark the end of the deployment,
	// collecting events along the way
	events := []*deployer.DeploymentEvent{}
loop:
	for {
		select {
		case e := <-stream.Events:
			events = append(events, e)
			if e.ReleaseID == "" {
				break loop
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for deployment event")
		}
	}
	t.Assert(events, c.DeepEquals, []*deployer.DeploymentEvent{
		{ReleaseID: release.ID, JobType: "printer", JobState: "up"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "down"},
		{ReleaseID: release.ID, JobType: "printer", JobState: "up"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: "down"},
		{ReleaseID: ""},
	})
}
