package main

import (
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/stream"
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
		case <-time.After(15 * time.Second):
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

func (s *DeployerSuite) TestServiceEvents(t *c.C) {
	deployment := s.createDeployment(t, "echoer", "all-at-once")
	events := make(chan *ct.DeploymentEvent)
	stream, err := s.controllerClient(t).StreamDeployment(deployment.ID, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	releaseID := deployment.NewReleaseID
	oldReleaseID := deployment.OldReleaseID

	expected := []*ct.DeploymentEvent{
		{ReleaseID: releaseID, JobType: "echoer", JobState: "starting", Status: "running"},
		{ReleaseID: releaseID, JobType: "echoer", JobState: "starting", Status: "running"},
		{ReleaseID: releaseID, JobType: "echoer", JobState: "up", Status: "running"},
		{ReleaseID: releaseID, JobType: "echoer", JobState: "up", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "echoer", JobState: "stopping", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "echoer", JobState: "stopping", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "echoer", JobState: "down", Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "echoer", JobState: "down", Status: "running"},
		{ReleaseID: releaseID, JobType: "", JobState: "", Status: "complete"},
	}
	waitForDeploymentEvents(t, events, expected)
}

func (s *DeployerSuite) assertRolledBack(t *c.C, deployment *ct.Deployment, processes map[string]int) {
	client := s.controllerClient(t)

	// check that we're running the old release
	release, err := client.GetAppRelease(deployment.AppID)
	t.Assert(err, c.IsNil)
	t.Assert(release.ID, c.Equals, deployment.OldReleaseID)

	// check that the old formation is the same and there's no new formation
	formation, err := client.GetFormation(deployment.AppID, deployment.OldReleaseID)
	t.Assert(err, c.IsNil)
	t.Assert(formation.Processes, c.DeepEquals, processes)
	_, err = client.GetFormation(deployment.AppID, deployment.NewReleaseID)
	t.Assert(err, c.NotNil)
}

func (s *DeployerSuite) TestRollbackFailedJob(t *c.C) {
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

	s.assertRolledBack(t, deployment, map[string]int{"printer": 2})
}

func (s *DeployerSuite) TestRollbackNoService(t *c.C) {
	// create a running release
	app, release := s.createRelease(t, "printer", "all-at-once")

	// deploy a release which will not register the service
	client := s.controllerClient(t)
	release.ID = ""
	printer := release.Processes["printer"]
	printer.Service = "printer"
	printer.Ports = []ct.Port{{
		Port:  12345,
		Proto: "tcp",
		Service: &host.Service{
			Name:   "printer",
			Create: true,
			Check: &host.HealthCheck{
				Type:         "tcp",
				Interval:     100 * time.Millisecond,
				Threshold:    1,
				KillDown:     true,
				StartTimeout: 100 * time.Millisecond,
			},
		},
	}}
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
		{ReleaseID: release.ID, JobType: "printer", JobState: "down", Status: "running"},
		{ReleaseID: release.ID, JobType: "", JobState: "", Status: "failed"},
	}
	waitForDeploymentEvents(t, events, expected)

	s.assertRolledBack(t, deployment, map[string]int{"printer": 2})
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
				debug(t, "reconnecting deployment event stream")
				connectStream()
				continue
			}
			debugf(t, "got deployment event: %s %s", e.JobType, e.JobState)
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

func (s *DeployerSuite) TestOmniProcess(t *c.C) {
	if testCluster == nil {
		t.Skip("cannot determine test cluster size")
	}

	// create and scale an omni release
	omniScale := 2
	totalJobs := omniScale * testCluster.Size()
	client := s.controllerClient(t)
	app, release := s.createApp(t)
	jEvents := make(chan *ct.JobEvent)
	jobStream, err := client.StreamJobEvents(app.Name, 0, jEvents)
	t.Assert(err, c.IsNil)
	defer jobStream.Close()
	t.Assert(client.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"omni": omniScale},
	}), c.IsNil)
	waitForJobEvents(t, jobStream, jEvents, jobEvents{"omni": {"up": totalJobs}})

	// deploy using all-at-once and check we get the correct events
	app.Strategy = "all-at-once"
	t.Assert(client.UpdateApp(app), c.IsNil)
	release.ID = ""
	t.Assert(client.CreateRelease(release), c.IsNil)
	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	events := make(chan *ct.DeploymentEvent)
	stream, err := client.StreamDeployment(deployment.ID, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	expected := make([]*ct.DeploymentEvent, 0, 4*totalJobs+1)
	appendEvents := func(releaseID, state string, count int) {
		for i := 0; i < count; i++ {
			event := &ct.DeploymentEvent{
				ReleaseID: releaseID,
				JobType:   "omni",
				JobState:  state,
				Status:    "running",
			}
			expected = append(expected, event)
		}
	}
	appendEvents(deployment.NewReleaseID, "starting", totalJobs)
	appendEvents(deployment.NewReleaseID, "up", totalJobs)
	appendEvents(deployment.OldReleaseID, "stopping", totalJobs)
	appendEvents(deployment.OldReleaseID, "down", totalJobs)
	expected = append(expected, &ct.DeploymentEvent{ReleaseID: deployment.NewReleaseID, Status: "complete"})
	waitForDeploymentEvents(t, events, expected)

	// deploy using one-by-one and check we get the correct events
	app.Strategy = "one-by-one"
	t.Assert(client.UpdateApp(app), c.IsNil)
	release.ID = ""
	t.Assert(client.CreateRelease(release), c.IsNil)
	deployment, err = client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	events = make(chan *ct.DeploymentEvent)
	stream, err = client.StreamDeployment(deployment.ID, events)
	t.Assert(err, c.IsNil)
	expected = make([]*ct.DeploymentEvent, 0, 4*totalJobs+1)
	appendEvents(deployment.NewReleaseID, "starting", testCluster.Size())
	appendEvents(deployment.NewReleaseID, "up", testCluster.Size())
	appendEvents(deployment.OldReleaseID, "stopping", testCluster.Size())
	appendEvents(deployment.OldReleaseID, "down", testCluster.Size())
	appendEvents(deployment.NewReleaseID, "starting", testCluster.Size())
	appendEvents(deployment.NewReleaseID, "up", testCluster.Size())
	appendEvents(deployment.OldReleaseID, "stopping", testCluster.Size())
	appendEvents(deployment.OldReleaseID, "down", testCluster.Size())
	expected = append(expected, &ct.DeploymentEvent{ReleaseID: deployment.NewReleaseID, Status: "complete"})
	waitForDeploymentEvents(t, events, expected)
}
