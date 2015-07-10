package main

import (
	"fmt"
	"testing"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	. "github.com/flynn/flynn/controller/testutils"
	ct "github.com/flynn/flynn/controller/types"
	//"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/random"
)

func Test(t *testing.T) { TestingT(t) }

type TestSuite struct{}

var _ = Suite(&TestSuite{})

const (
	testHostID     = "host-1"
	testArtifactId = "artifact-1"
	testReleaseID  = "release-1"
)

func createTestScheduler(appID string, processes map[string]int) *Scheduler {
	artifact := &ct.Artifact{ID: testArtifactId}
	release := NewRelease(testReleaseID, artifact, processes)
	h := NewFakeHostClient(testHostID)
	cluster := NewFakeCluster()
	cluster.SetHosts(map[string]*FakeHostClient{h.ID(): h})
	cc := NewFakeControllerClient(appID, release, artifact, processes)

	return NewScheduler(cluster, cc)
}

func waitForEvent(events chan Event, typ EventType) (Event, error) {
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return nil, fmt.Errorf("unexpected close of scheduler event stream")
			}
			if event.Type() == typ {
				if err := event.Err(); err != nil {
					return nil, fmt.Errorf("unexpected event error: %s", err)
				}
				return event, nil
			}
		case <-time.After(2 * time.Second):
			return nil, fmt.Errorf("timed out waiting for %s event", typ)
		}
	}
}

func (ts *TestSuite) TestInitialClusterSync(c *C) {
	s := createTestScheduler("testApp", map[string]int{"web": 1})

	events := make(chan Event, eventBufferSize)
	stream := s.Subscribe(events)
	defer s.Stop()
	defer stream.Close()
	go s.Run()

	// wait for a cluster sync event
	_, err := waitForEvent(events, EventTypeClusterSync)
	c.Assert(err, IsNil)

	// Ensure that the scheduler initializes the formation correctly and starts its jobs
	e, err := waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	event, ok := e.(*JobStartEvent)
	c.Assert(ok, Equals, true)
	c.Assert(event.Job, NotNil)
	job := event.Job
	c.Assert(job.Type, Equals, "web")
	c.Assert(job.AppID, Equals, "testApp")
	c.Assert(job.ReleaseID, Equals, testReleaseID)

	// Query the scheduler for the same job
	jobs := s.Jobs()
	c.Assert(jobs, HasLen, 1)
	for _, job := range jobs {
		c.Assert(job.Type, Equals, "web")
		c.Assert(job.HostID, Equals, testHostID)
		c.Assert(job.AppID, Equals, "testApp")
	}
}

func (ts *TestSuite) TestFormationChange(c *C) {
	app := &ct.App{ID: random.UUID(), Name: "test-formation-change"}
	s := createTestScheduler(app.ID, map[string]int{"web": 0})
	events := make(chan Event, eventBufferSize)
	stream := s.Subscribe(events)
	defer s.Stop()
	defer stream.Close()
	go s.Run()

	release, err := s.GetRelease(testReleaseID)
	c.Assert(err, IsNil)
	artifact, err := s.GetArtifact(release.ArtifactID)
	c.Assert(err, IsNil)

	s.formationChange <- &ct.ExpandedFormation{
		App:       app,
		Release:   release,
		Artifact:  artifact,
		Processes: map[string]int{"web": 2},
	}

	_, err = waitForEvent(events, EventTypeFormationChange)
	c.Assert(err, IsNil)
	e, err := waitForEvent(events, EventTypeJobStart)
	c.Assert(err, IsNil)
	event, ok := e.(*JobStartEvent)
	c.Assert(ok, Equals, true)
	c.Assert(event.Job, NotNil)
	job := event.Job
	c.Assert(job.Type, Equals, "web")
	c.Assert(job.AppID, Equals, app.ID)
	c.Assert(job.ReleaseID, Equals, testReleaseID)
}
