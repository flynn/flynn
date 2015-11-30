package main

import (
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
)

// Prefix the suite with "Z" so that it runs after all other tests
type ZDiscoverdSuite struct {
	Helper
}

var _ = c.Suite(&ZDiscoverdSuite{})

func (s *ZDiscoverdSuite) TestDeploy(t *c.C) {
	client := s.controllerClient(t)
	app, err := client.GetApp("discoverd")
	t.Assert(err, c.IsNil)
	release, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)
	release.ID = ""
	t.Assert(client.CreateRelease(release), c.IsNil)
	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)

	events := make(chan *ct.DeploymentEvent)
	stream, err := client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

loop:
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("unexpected close of deployment event stream")
			}
			if event.Status == "complete" {
				debugf(t, "got deployment event: %s", event.Status)
				break loop
			}
			if event.Status == "failed" {
				t.Fatal("the deployment failed")
			}
			debugf(t, "got deployment event: %s %s", event.JobType, event.JobState)
		case <-time.After(time.Duration(app.DeployTimeout) * time.Second):
			t.Fatal("timed out waiting for deployment event")
		}
	}
}
