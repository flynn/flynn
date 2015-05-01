package main

import (
	"testing"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/host/types"
)

func Test(t *testing.T) { TestingT(t) }

type TestSuite struct{}

var _ = Suite(&TestSuite{})

type FakeCluster struct {
	hosts []Host
}

func (f *FakeCluster) ListHosts() ([]Host, error) {
	return f.hosts, nil
}

type FakeHost struct {
	id   string
	jobs map[string]host.ActiveJob
}

func (f *FakeHost) ID() string {
	return f.id
}

func (f *FakeHost) ListJobs() (map[string]host.ActiveJob, error) {
	return f.jobs, nil
}

func (TestSuite) TestInitialClusterSync(c *C) {
	jobID := "job-1"
	host := &FakeHost{id: "host-1", jobs: map[string]host.ActiveJob{
		jobID: {Job: &host.Job{ID: jobID}},
	}}
	cluster := &FakeCluster{hosts: []Host{host}}

	s := NewScheduler(cluster)
	events := make(chan *Event)
	stream := s.Subscribe(events)
	defer stream.Close()
	go s.Run()
	defer s.Stop()

	// wait for a cluster sync event
loop:
	for {
		select {
		case event, ok := <-events:
			if !ok {
				c.Fatal("unexpected close of scheduler event stream")
			}
			if event.Type == EventTypeClusterSync {
				break loop
			}
		case <-time.After(time.Second):
			c.Fatal("timed out waiting for cluster sync event")
		}
	}

	// check the scheduler has the job
	job := s.GetJob(jobID)
	c.Assert(job, NotNil)
	c.Assert(job.Job.ID, Equals, jobID)
}

func (TestSuite) TestFormationChange(c *C) {
}
