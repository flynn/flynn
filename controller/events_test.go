package main

import (
	"encoding/json"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
)

func (s *S) TestAppEvents(c *C) {
	app1 := s.createTestApp(c, &ct.App{Name: "app1"})
	app2 := s.createTestApp(c, &ct.App{Name: "app2"})
	release := s.createTestRelease(c, &ct.Release{})

	jobID1 := "host-job1"
	jobID2 := "host-job2"
	jobID3 := "host-job3"
	jobs := []*ct.Job{
		{ID: jobID1, AppID: app1.ID, ReleaseID: release.ID, Type: "web", State: "starting"},
		{ID: jobID1, AppID: app1.ID, ReleaseID: release.ID, Type: "web", State: "up"},
		{ID: jobID2, AppID: app1.ID, ReleaseID: release.ID, Type: "web", State: "starting"},
		{ID: jobID2, AppID: app1.ID, ReleaseID: release.ID, Type: "web", State: "up"},
		{ID: jobID3, AppID: app2.ID, ReleaseID: release.ID, Type: "web", State: "starting"},
		{ID: jobID3, AppID: app2.ID, ReleaseID: release.ID, Type: "web", State: "up"},
	}

	listener := newEventListener(&AppRepo{db: s.hc.db})
	c.Assert(listener.Listen(), IsNil)

	// sub1 should receive job events for app1, job1
	sub1, err := listener.Subscribe(app1.ID, string(ct.EventTypeJob), jobID1)
	c.Assert(err, IsNil)
	defer sub1.Close()

	// sub2 should receive all job events for app1
	sub2, err := listener.Subscribe(app1.ID, string(ct.EventTypeJob), "")
	c.Assert(err, IsNil)
	defer sub2.Close()

	// sub3 should receive all job events for app2
	sub3, err := listener.Subscribe(app2.ID, "", "")
	c.Assert(err, IsNil)
	defer sub3.Close()

	for _, job := range jobs {
		s.createTestJob(c, job)
	}

	assertJobEvents := func(sub *EventSubscriber, expected []*ct.Job) {
		var index int
		for {
			select {
			case e, ok := <-sub.Events:
				if !ok {
					c.Fatalf("unexpected close of event stream: %s", sub.Err)
				}
				var jobEvent ct.JobEvent
				c.Assert(json.Unmarshal(e.Data, &jobEvent), IsNil)
				job := expected[index]
				c.Assert(jobEvent, DeepEquals, ct.JobEvent{
					JobID:     job.ID,
					AppID:     job.AppID,
					ReleaseID: job.ReleaseID,
					Type:      job.Type,
					State:     job.State,
				})
				index += 1
				if index == len(expected) {
					return
				}
			case <-time.After(time.Second):
				c.Fatal("timed out waiting for app event")
			}
		}
	}
	assertJobEvents(sub1, jobs[0:2])
	assertJobEvents(sub2, jobs[0:4])
	assertJobEvents(sub3, jobs[4:6])
}
