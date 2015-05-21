package main

import (
	"fmt"
	"testing"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	. "github.com/flynn/flynn/controller/testutils"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/random"
)

func Test(t *testing.T) { TestingT(t) }

type TestSuite struct{}

var _ = Suite(&TestSuite{})

func createTestScheduler(appID string, processes map[string]int) *Scheduler {
	artifact := &ct.Artifact{ID: "artifact-1"}
	release := NewRelease("release-1", artifact, processes)
	h := NewFakeHostClient("host-1")
	for k, c := range processes {
		for i := 0; i < c; i++ {
			jobID := random.UUID()
			h.AddJob(&host.Job{
				ID: jobID,
				Metadata: map[string]string{
					"flynn-controller.app":     appID,
					"flynn-controller.release": release.ID,
					"flynn-controller.type":    k,
				},
				Artifact: host.Artifact{
					Type: artifact.Type,
					URI:  artifact.URI,
				},
			})
		}
	}
	cluster := NewFakeCluster()
	cluster.SetHosts(map[string]*FakeHostClient{h.ID(): h})
	cc := NewFakeControllerClient(appID, release, artifact, processes)

	return NewScheduler(cluster, cc)
}

func waitForEventType(events chan Event, etype EventType) (Event, error) {
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return nil, fmt.Errorf("unexpected close of scheduler event stream")
			}
			if event.Type() == etype {
				if event.Err() != nil {
					return nil, fmt.Errorf("Error occurred occurred while processing event of type %q. Error: %v", etype, event.Err())
				}
				return event, nil
			}
		case <-time.After(time.Second):
			return nil, fmt.Errorf("timed out waiting for cluster sync event")
		}
	}
}

func (ts *TestSuite) TestInitialClusterSync(c *C) {
	s := createTestScheduler("testApp", map[string]int{"web": 1})

	events := make(chan Event)
	stream := s.Subscribe(events)
	defer stream.Close()
	go s.Run()
	defer s.Stop()

	// wait for a cluster sync event
	_, err := waitForEventType(events, EventTypeClusterSync)
	fatalIfError(c, err)

	// check the scheduler has the job
	formation, err := s.getFormation("testApp", "testApp", "release-1")
	jobs := formation.GetJobsForType("web")
	c.Assert(err, IsNil)
	c.Assert(jobs, NotNil)
	for _, j := range jobs {
		c.Assert(j.HostID, Equals, "host-1")
	}
	c.Assert(len(jobs), Equals, 1)
}

func (ts *TestSuite) TestFormationChange(c *C) {
	s := createTestScheduler("testApp", map[string]int{"web": 1})
	release, _ := s.GetRelease("release-1")
	artifact, _ := s.GetArtifact(release.ArtifactID)
	s.log.Info("Testing formation change")

	events := make(chan Event, 1)
	stream := s.Subscribe(events)
	defer stream.Close()
	go s.Run()
	defer s.Stop()

	// wait for a cluster sync event
	_, err := waitForEventType(events, EventTypeClusterSync)
	fatalIfError(c, err)

	s.formationChange <- &ct.ExpandedFormation{
		App: &ct.App{
			Name: "test-formation-change",
			ID:   "test-formation-change",
		},
		Release:   release,
		Artifact:  artifact,
		Processes: map[string]int{"web": 2},
	}

	_, err = waitForEventType(events, EventTypeFormationChange)
	fatalIfError(c, err)
	e, err := waitForEventType(events, EventTypeJobStart)
	fatalIfError(c, err)
	je, ok := e.(*JobStartEvent)
	c.Assert(ok, Equals, true)
	c.Assert(je.Job, NotNil)

	s.stop <- struct{}{}
}

func fatalIfError(c *C, err error) {
	if err != nil {
		c.Fatal(err.Error())
	}
}
