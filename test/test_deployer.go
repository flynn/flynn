package main

import (
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/stream"
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

func (s *DeployerSuite) TestDeployController(t *c.C) {
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

	// use a function to create the event stream as a new stream will be needed
	// after deploying the controller
	var events chan *ct.DeploymentEvent
	var eventStream stream.Stream
	connectStream := func() {
		events = make(chan *ct.DeploymentEvent)
		err := attempt.Strategy{
			Total: 10 * time.Second,
			Delay: 500 * time.Millisecond,
		}.Run(func() (err error) {
			eventStream, err = client.StreamDeployment(deployment.ID, events)
			return
		})
		t.Assert(err, c.IsNil)
	}
	connectStream()
	defer eventStream.Close()

	// wait for the deploy to complete (this doesn't wait for specific events
	// due to the fact that when the deployer deploys itself, some events will
	// not get sent)
loop:
	for {
		select {
		case e, ok := <-events:
			if !ok {
				// reconnect the stream as it may of been closed
				// due to the controller being deployed
				connectStream()
				continue
			}
			switch e.Status {
			case "complete":
				break loop
			case "failed":
				t.Fatal("the deployment failed")
			}
		case <-time.After(20 * time.Second):
			t.Fatal("timed out waiting for the deploy to complete")
		}
	}

	// check the correct controller jobs are running
	hosts, err := s.clusterClient(t).ListHosts()
	t.Assert(err, c.IsNil)
	actual := make(map[string]map[string]int)
	for _, host := range hosts {
		for _, job := range host.Jobs {
			appID := job.Metadata["flynn-controller.app"]
			if appID != app.ID {
				continue
			}
			releaseID := job.Metadata["flynn-controller.release"]
			if _, ok := actual[releaseID]; !ok {
				actual[releaseID] = make(map[string]int)
			}
			typ := job.Metadata["flynn-controller.type"]
			actual[releaseID][typ]++
		}
	}
	expected := map[string]map[string]int{release.ID: {
		"web":       1,
		"deployer":  1,
		"scheduler": testCluster.Size(),
	}}
	t.Assert(actual, c.DeepEquals, expected)
}
