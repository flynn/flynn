package main

import (
	"time"

	ct "github.com/flynn/flynn/controller/types"
	discoverd "github.com/flynn/flynn/discoverd/client"
	host "github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/stream"
	c "github.com/flynn/go-check"
)

type DeployerSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&DeployerSuite{})

type testDeploy struct {
	s            *DeployerSuite
	t            *c.C
	deployment   *ct.Deployment
	deployEvents chan *ct.DeploymentEvent
	deployStream stream.Stream
	jobEvents    chan *ct.Job
	jobStream    stream.Stream
}

func (t *testDeploy) cleanup() {
	t.jobStream.Close()
	t.deployStream.Close()
}

func (s *DeployerSuite) createRelease(t *c.C, process, strategy string, scale int) (*ct.App, *ct.Release) {
	app, release := s.createApp(t)
	app.Strategy = strategy
	t.Assert(s.controllerClient(t).UpdateApp(app), c.IsNil)

	watcher, err := s.controllerClient(t).WatchJobEvents(app.Name, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	t.Assert(s.controllerClient(t).PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{process: scale},
	}), c.IsNil)

	err = watcher.WaitFor(ct.JobEvents{process: {ct.JobStateUp: scale}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)

	return app, release
}

func (s *DeployerSuite) createDeployment(t *c.C, process, strategy, service string, scale int) *testDeploy {
	app, release := s.createRelease(t, process, strategy, scale)
	return s.createDeploymentWithApp(t, app, release, service, scale)
}

func (s *DeployerSuite) createDeploymentWithApp(t *c.C, app *ct.App, release *ct.Release, service string, scale int) *testDeploy {
	if service != "" {
		debugf(t, "waiting for %d %s services", scale, service)
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
				if count == scale {
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

	client := s.controllerClient(t)
	jobEvents := make(chan *ct.Job)
	jobStream, err := client.StreamJobEvents(app.ID, jobEvents)
	t.Assert(err, c.IsNil)

	// create a new release for the deployment
	release.ID = ""
	t.Assert(client.CreateRelease(app.ID, release), c.IsNil)

	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	debugf(t, "created deployment %s", deployment.ID)
	debugf(t, "deploying from release %s to %s", deployment.OldReleaseID, deployment.NewReleaseID)

	deployEvents := make(chan *ct.DeploymentEvent)
	deployStream, err := client.StreamDeployment(deployment, deployEvents)
	t.Assert(err, c.IsNil)

	return &testDeploy{
		s:            s,
		t:            t,
		deployment:   deployment,
		deployEvents: deployEvents,
		deployStream: deployStream,
		jobEvents:    jobEvents,
		jobStream:    jobStream,
	}
}

func (t *testDeploy) waitForDeploymentStatus(status string) {
	t.s.waitForDeploymentStatus(t.t, t.deployEvents, status)
}

func (s *DeployerSuite) waitForDeploymentStatus(t *c.C, events chan *ct.DeploymentEvent, status string) *ct.DeploymentEvent {
	for {
		select {
		case event := <-events:
			// ignore pending status
			if event.Status == "pending" {
				continue
			}
			if event.Status != status {
				t.Fatalf("expected deploy %s event, got %s", status, event.Status)
			}
			return event
		case <-time.After(60 * time.Second):
			t.Fatalf("timed out waiting for deploy %s event", status)
		}
	}
	return nil
}

func (t *testDeploy) waitForJobEvents(typ string, expected []*ct.Job) {
	t.s.waitForJobEvents(t.t, typ, t.jobEvents, expected)
}

func (s *DeployerSuite) waitForJobEvents(t *c.C, jobType string, events chan *ct.Job, expected []*ct.Job) {
	debugf(t, "waiting for %d job events", len(expected))
	actual := make([]*ct.Job, 0, len(expected))
loop:
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("unexpected close of job event stream")
			}
			// only track up and down events as we can't always
			// predict the order of pending / starting / stopping
			// events when scaling multiple jobs
			if e.State != ct.JobStateUp && e.State != ct.JobStateDown {
				continue
			}
			debugf(t, "got job event: job.id: %s release.id: %s state: %v", e.ID, e.ReleaseID, e.State)
			actual = append(actual, e)
			if len(actual) == len(expected) {
				break loop
			}
		case <-time.After(60 * time.Second):
			t.Fatal("timed out waiting for job events")
		}
	}
	for i, event := range expected {
		t.Assert(actual[i].ReleaseID, c.Equals, event.ReleaseID)
		t.Assert(actual[i].State, c.Equals, event.State)
		t.Assert(actual[i].Type, c.Equals, jobType)
	}
}

func (s *DeployerSuite) TestOneByOneStrategy(t *c.C) {
	d := s.createDeployment(t, "printer", "one-by-one", "", 2)
	defer d.cleanup()
	releaseID := d.deployment.NewReleaseID
	oldReleaseID := d.deployment.OldReleaseID

	d.waitForJobEvents("printer", []*ct.Job{
		{ReleaseID: releaseID, State: ct.JobStateUp},
		{ReleaseID: oldReleaseID, State: ct.JobStateDown},
		{ReleaseID: releaseID, State: ct.JobStateUp},
		{ReleaseID: oldReleaseID, State: ct.JobStateDown},
	})
	d.waitForDeploymentStatus("complete")
}

// TestInBatchesDefaultStrategy tests that deployments using the in-batches
// strategy without an explicit batch size set defaults to using the host
// count as the batch size
func (s *DeployerSuite) TestInBatchesDefaultStrategy(t *c.C) {
	// get the host count
	hosts, err := s.clusterClient(t).Hosts()
	t.Assert(err, c.IsNil)

	d := s.createDeployment(t, "printer", "in-batches", "", len(hosts)*3)
	defer d.cleanup()
	releaseID := d.deployment.NewReleaseID
	oldReleaseID := d.deployment.OldReleaseID

	expectedEvents := make([]*ct.Job, 0, len(hosts)*3)
	for i := 0; i < 3; i++ {
		for range hosts {
			expectedEvents = append(expectedEvents, &ct.Job{
				ReleaseID: releaseID,
				State:     ct.JobStateUp,
			})
		}
		for range hosts {
			expectedEvents = append(expectedEvents, &ct.Job{
				ReleaseID: oldReleaseID,
				State:     ct.JobStateDown,
			})
		}
	}
	d.waitForJobEvents("printer", expectedEvents)
	d.waitForDeploymentStatus("complete")
}

// TestInBatchesStrategy tests the in-batches deployment strategy with an
// explicit batch-size
func (s *DeployerSuite) TestInBatchesStrategy(t *c.C) {
	batchSize := 4
	scale := batchSize * 2
	app, release := s.createRelease(t, "printer", "in-batches", scale)
	app.SetDeployBatchSize(batchSize)
	t.Assert(s.controllerClient(t).UpdateApp(app), c.IsNil)
	d := s.createDeploymentWithApp(t, app, release, "", scale)
	defer d.cleanup()

	releaseID := d.deployment.NewReleaseID
	oldReleaseID := d.deployment.OldReleaseID

	expectedEvents := make([]*ct.Job, 0, scale*2)
	for i := 0; i < 2; i++ {
		for i := 0; i < batchSize; i++ {
			expectedEvents = append(expectedEvents, &ct.Job{
				ReleaseID: releaseID,
				State:     ct.JobStateUp,
			})
		}
		for i := 0; i < batchSize; i++ {
			expectedEvents = append(expectedEvents, &ct.Job{
				ReleaseID: oldReleaseID,
				State:     ct.JobStateDown,
			})
		}
	}
	d.waitForJobEvents("printer", expectedEvents)
	d.waitForDeploymentStatus("complete")
}

func (s *DeployerSuite) TestOneDownOneUpStrategy(t *c.C) {
	d := s.createDeployment(t, "printer", "one-down-one-up", "", 2)
	defer d.cleanup()
	releaseID := d.deployment.NewReleaseID
	oldReleaseID := d.deployment.OldReleaseID

	d.waitForJobEvents("printer", []*ct.Job{
		{ReleaseID: oldReleaseID, State: ct.JobStateDown},
		{ReleaseID: releaseID, State: ct.JobStateUp},
		{ReleaseID: oldReleaseID, State: ct.JobStateDown},
		{ReleaseID: releaseID, State: ct.JobStateUp},
	})
	d.waitForDeploymentStatus("complete")
}

func (s *DeployerSuite) TestAllAtOnceStrategy(t *c.C) {
	d := s.createDeployment(t, "printer", "all-at-once", "", 2)
	defer d.cleanup()
	releaseID := d.deployment.NewReleaseID
	oldReleaseID := d.deployment.OldReleaseID

	d.waitForJobEvents("printer", []*ct.Job{
		{ReleaseID: releaseID, State: ct.JobStateUp},
		{ReleaseID: releaseID, State: ct.JobStateUp},
		{ReleaseID: oldReleaseID, State: ct.JobStateDown},
		{ReleaseID: oldReleaseID, State: ct.JobStateDown},
	})
	d.waitForDeploymentStatus("complete")
}

func (s *DeployerSuite) TestServiceEvents(t *c.C) {
	d := s.createDeployment(t, "echoer", "all-at-once", "echo-service", 2)
	defer d.cleanup()
	releaseID := d.deployment.NewReleaseID
	oldReleaseID := d.deployment.OldReleaseID

	d.waitForJobEvents("echoer", []*ct.Job{
		{ReleaseID: releaseID, State: ct.JobStateUp},
		{ReleaseID: releaseID, State: ct.JobStateUp},
		{ReleaseID: oldReleaseID, State: ct.JobStateDown},
		{ReleaseID: oldReleaseID, State: ct.JobStateDown},
	})
	d.waitForDeploymentStatus("complete")
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
	app, release := s.createRelease(t, "printer", "all-at-once", 2)

	// deploy a release which will fail to start
	client := s.controllerClient(t)
	release.ID = ""
	printer := release.Processes["printer"]
	printer.Args = []string{"this-is-gonna-fail"}
	release.Processes["printer"] = printer
	t.Assert(client.CreateRelease(app.ID, release), c.IsNil)
	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)

	// check the deployment fails
	events := make(chan *ct.DeploymentEvent)
	stream, err := client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	event := s.waitForDeploymentStatus(t, events, "failed")
	t.Assert(event.Error, c.Equals, `printer job failed to start: exec: "this-is-gonna-fail": executable file not found in $PATH`)

	s.assertRolledBack(t, deployment, map[string]int{"printer": 2})
}

func (s *DeployerSuite) TestRollbackNoService(t *c.C) {
	// create a running release
	app, release := s.createRelease(t, "printer", "all-at-once", 2)

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
	t.Assert(client.CreateRelease(app.ID, release), c.IsNil)
	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)

	// check the deployment fails
	events := make(chan *ct.DeploymentEvent)
	stream, err := client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	event := s.waitForDeploymentStatus(t, events, "failed")
	t.Assert(event.Error, c.Equals, "printer job failed to start: got down job event")

	s.assertRolledBack(t, deployment, map[string]int{"printer": 2})

	// check a new deployment can be created
	_, err = client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)
}

func (s *DeployerSuite) TestOmniProcess(t *c.C) {
	clusterSize := 3
	x := s.bootCluster(t, clusterSize)
	defer x.Destroy()

	// create and scale an omni release
	omniScale := 2
	totalJobs := omniScale * clusterSize
	client := x.controller
	app, release := s.createAppWithClient(t, client)

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
	t.Assert(client.CreateRelease(app.ID, release), c.IsNil)
	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	events := make(chan *ct.DeploymentEvent)
	stream, err := client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	expected := make([]*ct.Job, 0, 3*totalJobs+1)
	appendEvents := func(releaseID string, state ct.JobState, count int) {
		for i := 0; i < count; i++ {
			expected = append(expected, &ct.Job{
				ReleaseID: releaseID,
				Type:      "omni",
				State:     state,
			})
		}
	}
	appendEvents(deployment.NewReleaseID, ct.JobStateUp, totalJobs)
	appendEvents(deployment.OldReleaseID, ct.JobStateDown, totalJobs)
	s.waitForDeploymentStatus(t, events, "complete")

	// deploy using one-by-one and check we get the correct events
	app.Strategy = "one-by-one"
	t.Assert(client.UpdateApp(app), c.IsNil)
	release.ID = ""
	t.Assert(client.CreateRelease(app.ID, release), c.IsNil)
	// try creating the deployment multiple times to avoid getting a
	// "Cannot create deploy, one is already in progress" error (there
	// is no guarantee the previous deploy has finished yet)
	attempts := attempt.Strategy{Total: 10 * time.Second, Delay: 100 * time.Millisecond}
	err = attempts.Run(func() (err error) {
		deployment, err = client.CreateDeployment(app.ID, release.ID)
		return
	})
	t.Assert(err, c.IsNil)
	events = make(chan *ct.DeploymentEvent)
	stream, err = client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()
	expected = make([]*ct.Job, 0, 4*totalJobs+1)
	appendEvents(deployment.NewReleaseID, ct.JobStateUp, clusterSize)
	appendEvents(deployment.OldReleaseID, ct.JobStateDown, clusterSize)
	appendEvents(deployment.NewReleaseID, ct.JobStateUp, clusterSize)
	appendEvents(deployment.OldReleaseID, ct.JobStateDown, clusterSize)
	s.waitForDeploymentStatus(t, events, "complete")
}
