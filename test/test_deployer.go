package main

import (
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	c "github.com/flynn/go-check"
)

type DeployerSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&DeployerSuite{})

func (s *DeployerSuite) createRelease(t *c.C, process, strategy string) (*ct.App, *ct.Release) {
	app, release := s.createApp(t)
	app.Strategy = strategy
	s.controllerClient(t).UpdateApp(app)

	watcher, err := s.controllerClient(t).WatchJobEvents(app.Name, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	t.Assert(s.controllerClient(t).PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{process: 2},
	}), c.IsNil)

	err = watcher.WaitFor(ct.JobEvents{process: {ct.JobStateUp: 2}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)

	return app, release
}

func (s *DeployerSuite) createDeployment(t *c.C, process, strategy, service string) *ct.Deployment {
	app, release := s.createRelease(t, process, strategy)

	if service != "" {
		debugf(t, "waiting for 2 %s services", service)
		events := make(chan *discoverd.Event)
		stream, err := s.discoverdClient(t).Service(service).Watch(events)
		t.Assert(err, c.IsNil)
		defer stream.Close()
		count := 0
	loop:
		for {
			select {
			case event, ok := <-events:
				if !ok {
					t.Fatalf("service discovery stream closed unexpectedly")
				}
				if event.Kind == discoverd.EventKindUp {
					if id, ok := event.Instance.Meta["FLYNN_RELEASE_ID"]; !ok || id != release.ID {
						continue
					}
					debugf(t, "got %s service up event", service)
					count++
				}
				if count == 2 {
					// although the services are up, give them a few more seconds
					// to make sure the deployer will also see them as up.
					time.Sleep(5 * time.Second)
					break loop
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("timed out waiting for %s service to come up", service)
			}
		}
	}

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
		case e, ok := <-stream:
			if !ok {
				t.Fatal("unexpected close of deployment event stream")
			}
			actual = append(actual, e)
			if e.Status == "complete" || e.Status == "failed" {
				debugf(t, "got deployment event: %s", e.Status)
				break loop
			}
			debugf(t, "got deployment event: %s %s", e.JobType, e.JobState)
		case <-time.After(60 * time.Second):
			t.Fatal("timed out waiting for deployment event")
		}
	}
	compare := func(t *c.C, i *ct.DeploymentEvent, j *ct.DeploymentEvent) {
		t.Assert(i.ReleaseID, c.Equals, j.ReleaseID)
		t.Assert(i.JobType, c.Equals, j.JobType)
		t.Assert(i.JobState, c.Equals, j.JobState)
		t.Assert(i.Status, c.Equals, j.Status)
		t.Assert(i.Error, c.Equals, j.Error)
	}

	for i, e := range expected {
		compare(t, actual[i], e)
	}
}

func (s *DeployerSuite) TestOneByOneStrategy(t *c.C) {
	deployment := s.createDeployment(t, "printer", "one-by-one", "")
	events := make(chan *ct.DeploymentEvent)
	stream, err := s.controllerClient(t).StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	releaseID := deployment.NewReleaseID
	oldReleaseID := deployment.OldReleaseID

	expected := []*ct.DeploymentEvent{
		{ReleaseID: releaseID, JobType: "", JobState: "", Status: "pending"},
		{ReleaseID: releaseID, JobType: "printer", JobState: ct.JobStateStarting, Status: "running"},
		{ReleaseID: releaseID, JobType: "printer", JobState: ct.JobStateUp, Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: ct.JobStateStopping, Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: ct.JobStateDown, Status: "running"},
		{ReleaseID: releaseID, JobType: "printer", JobState: ct.JobStateStarting, Status: "running"},
		{ReleaseID: releaseID, JobType: "printer", JobState: ct.JobStateUp, Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: ct.JobStateStopping, Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: ct.JobStateDown, Status: "running"},
		{ReleaseID: releaseID, JobType: "", JobState: "", Status: "complete"},
	}
	waitForDeploymentEvents(t, events, expected)
}

func (s *DeployerSuite) TestAllAtOnceStrategy(t *c.C) {
	deployment := s.createDeployment(t, "printer", "all-at-once", "")
	events := make(chan *ct.DeploymentEvent)
	stream, err := s.controllerClient(t).StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	releaseID := deployment.NewReleaseID
	oldReleaseID := deployment.OldReleaseID

	expected := []*ct.DeploymentEvent{
		{ReleaseID: releaseID, JobType: "", JobState: "", Status: "pending"},
		{ReleaseID: releaseID, JobType: "printer", JobState: ct.JobStateStarting, Status: "running"},
		{ReleaseID: releaseID, JobType: "printer", JobState: ct.JobStateStarting, Status: "running"},
		{ReleaseID: releaseID, JobType: "printer", JobState: ct.JobStateUp, Status: "running"},
		{ReleaseID: releaseID, JobType: "printer", JobState: ct.JobStateUp, Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: ct.JobStateStopping, Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: ct.JobStateStopping, Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: ct.JobStateDown, Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "printer", JobState: ct.JobStateDown, Status: "running"},
		{ReleaseID: releaseID, JobType: "", JobState: "", Status: "complete"},
	}
	waitForDeploymentEvents(t, events, expected)
}

func (s *DeployerSuite) TestServiceEvents(t *c.C) {
	deployment := s.createDeployment(t, "echoer", "all-at-once", "echo-service")
	events := make(chan *ct.DeploymentEvent)
	stream, err := s.controllerClient(t).StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	releaseID := deployment.NewReleaseID
	oldReleaseID := deployment.OldReleaseID

	expected := []*ct.DeploymentEvent{
		{ReleaseID: releaseID, JobType: "", JobState: "", Status: "pending"},
		{ReleaseID: releaseID, JobType: "echoer", JobState: ct.JobStateStarting, Status: "running"},
		{ReleaseID: releaseID, JobType: "echoer", JobState: ct.JobStateStarting, Status: "running"},
		{ReleaseID: releaseID, JobType: "echoer", JobState: ct.JobStateUp, Status: "running"},
		{ReleaseID: releaseID, JobType: "echoer", JobState: ct.JobStateUp, Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "echoer", JobState: ct.JobStateStopping, Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "echoer", JobState: ct.JobStateStopping, Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "echoer", JobState: ct.JobStateDown, Status: "running"},
		{ReleaseID: oldReleaseID, JobType: "echoer", JobState: ct.JobStateDown, Status: "running"},
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
	printer.Args = []string{"this-is-gonna-fail"}
	release.Processes["printer"] = printer
	t.Assert(client.CreateRelease(release), c.IsNil)
	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)

	// check the deployment fails
	events := make(chan *ct.DeploymentEvent)
	stream, err := client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	expected := []*ct.DeploymentEvent{
		{ReleaseID: release.ID, JobType: "", JobState: "", Status: "pending"},
		{ReleaseID: release.ID, JobType: "printer", JobState: ct.JobStateStarting, Status: "running"},
		{ReleaseID: release.ID, JobType: "printer", JobState: ct.JobStateStarting, Status: "running"},
		{ReleaseID: release.ID, JobType: "printer", JobState: ct.JobStateDown, Status: "running"},
		{ReleaseID: release.ID, JobType: "", JobState: "", Status: "failed", Error: `deployer: printer job failed to start: exec: "this-is-gonna-fail": executable file not found in $PATH`},
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
	stream, err := client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	expected := []*ct.DeploymentEvent{
		{ReleaseID: release.ID, JobType: "", JobState: "", Status: "pending"},
		{ReleaseID: release.ID, JobType: "printer", JobState: ct.JobStateStarting, Status: "running"},
		{ReleaseID: release.ID, JobType: "printer", JobState: ct.JobStateStarting, Status: "running"},
		{ReleaseID: release.ID, JobType: "printer", JobState: ct.JobStateDown, Status: "running"},
		{ReleaseID: release.ID, JobType: "", JobState: "", Status: "failed", Error: "printer process type failed to start, got down job event"},
	}
	waitForDeploymentEvents(t, events, expected)

	s.assertRolledBack(t, deployment, map[string]int{"printer": 2})

	// check a new deployment can be created
	_, err = client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)
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

	watcher, err := client.WatchJobEvents(app.Name, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	t.Assert(client.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"omni": omniScale},
	}), c.IsNil)
	err = watcher.WaitFor(ct.JobEvents{"omni": {ct.JobStateUp: totalJobs}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)

	// deploy using all-at-once and check we get the correct events
	app.Strategy = "all-at-once"
	t.Assert(client.UpdateApp(app), c.IsNil)
	release.ID = ""
	t.Assert(client.CreateRelease(release), c.IsNil)
	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	events := make(chan *ct.DeploymentEvent)
	stream, err := client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	expected := make([]*ct.DeploymentEvent, 0, 4*totalJobs+1)
	appendEvents := func(releaseID string, state ct.JobState, count int) {
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
	expected = append(expected, &ct.DeploymentEvent{ReleaseID: deployment.NewReleaseID, Status: "pending"})
	appendEvents(deployment.NewReleaseID, ct.JobStateStarting, totalJobs)
	appendEvents(deployment.NewReleaseID, ct.JobStateUp, totalJobs)
	appendEvents(deployment.OldReleaseID, ct.JobStateStopping, totalJobs)
	appendEvents(deployment.OldReleaseID, ct.JobStateDown, totalJobs)
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
	stream, err = client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	expected = make([]*ct.DeploymentEvent, 0, 4*totalJobs+1)
	expected = append(expected, &ct.DeploymentEvent{ReleaseID: deployment.NewReleaseID, Status: "pending"})
	appendEvents(deployment.NewReleaseID, ct.JobStateStarting, testCluster.Size())
	appendEvents(deployment.NewReleaseID, ct.JobStateUp, testCluster.Size())
	appendEvents(deployment.OldReleaseID, ct.JobStateStopping, testCluster.Size())
	appendEvents(deployment.OldReleaseID, ct.JobStateDown, testCluster.Size())
	appendEvents(deployment.NewReleaseID, ct.JobStateStarting, testCluster.Size())
	appendEvents(deployment.NewReleaseID, ct.JobStateUp, testCluster.Size())
	appendEvents(deployment.OldReleaseID, ct.JobStateStopping, testCluster.Size())
	appendEvents(deployment.OldReleaseID, ct.JobStateDown, testCluster.Size())
	expected = append(expected, &ct.DeploymentEvent{ReleaseID: deployment.NewReleaseID, Status: "complete"})
	waitForDeploymentEvents(t, events, expected)
}
