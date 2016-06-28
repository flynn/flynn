package main

import (
	"encoding/json"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/random"
	. "github.com/flynn/go-check"
)

func (s *S) TestEvents(c *C) {
	app1 := s.createTestApp(c, &ct.App{Name: "app1"})
	app2 := s.createTestApp(c, &ct.App{Name: "app2"})
	release := s.createTestRelease(c, "", &ct.Release{})

	jobID1 := random.UUID()
	jobID2 := random.UUID()
	jobID3 := random.UUID()
	jobs := []*ct.Job{
		{UUID: jobID1, AppID: app1.ID, ReleaseID: release.ID, Type: "web", State: ct.JobStateStarting},
		{UUID: jobID1, AppID: app1.ID, ReleaseID: release.ID, Type: "web", State: ct.JobStateUp},
		{UUID: jobID2, AppID: app1.ID, ReleaseID: release.ID, Type: "web", State: ct.JobStateStarting},
		{UUID: jobID2, AppID: app1.ID, ReleaseID: release.ID, Type: "web", State: ct.JobStateUp},
		{UUID: jobID3, AppID: app2.ID, ReleaseID: release.ID, Type: "web", State: ct.JobStateStarting},
		{UUID: jobID3, AppID: app2.ID, ReleaseID: release.ID, Type: "web", State: ct.JobStateUp},
	}

	listener := newEventListener(&EventRepo{db: s.hc.db})
	c.Assert(listener.Listen(), IsNil)

	// sub1 should receive job events for app1, job1
	sub1, err := listener.Subscribe(app1.ID, []string{string(ct.EventTypeJob)}, jobID1)
	c.Assert(err, IsNil)
	defer sub1.Close()

	// sub2 should receive all job events for app1
	sub2, err := listener.Subscribe(app1.ID, []string{string(ct.EventTypeJob)}, "")
	c.Assert(err, IsNil)
	defer sub2.Close()

	// sub3 should receive all job events for app2
	sub3, err := listener.Subscribe(app2.ID, []string{}, "")
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
				var jobEvent ct.Job
				c.Assert(json.Unmarshal(e.Data, &jobEvent), IsNil)
				job := expected[index]
				c.Assert(jobEvent, DeepEquals, *job)
				index += 1
				if index == len(expected) {
					return
				}
			case <-time.After(10 * time.Second):
				c.Fatal("timed out waiting for app event")
			}
		}
	}
	assertJobEvents(sub1, jobs[0:2])
	assertJobEvents(sub2, jobs[0:4])
	assertJobEvents(sub3, jobs[4:6])
}

func (s *S) TestStreamAppLifeCycleEvents(c *C) {
	events := make(chan *ct.Event)
	stream, err := s.c.StreamEvents(ct.StreamEventsOptions{}, events)
	c.Assert(err, IsNil)
	defer stream.Close()

	app := s.createTestApp(c, &ct.App{Name: "app3"})
	release := s.createTestRelease(c, app.ID, &ct.Release{})
	nextRelease := s.createTestRelease(c, app.ID, &ct.Release{})

	c.Assert(s.c.SetAppRelease(app.ID, release.ID), IsNil)
	newStrategy := "one-by-one"
	c.Assert(s.c.UpdateApp(&ct.App{
		ID:       app.ID,
		Strategy: newStrategy,
	}), IsNil)
	newMeta := map[string]string{
		"foo": "bar",
	}
	c.Assert(s.c.UpdateApp(&ct.App{
		ID:   app.ID,
		Meta: newMeta,
	}), IsNil)

	c.Assert(s.c.SetAppRelease(app.ID, nextRelease.ID), IsNil)

	assertAppEvent := func(e *ct.Event) *ct.App {
		var eventApp *ct.App
		c.Assert(json.Unmarshal(e.Data, &eventApp), IsNil)
		c.Assert(e.ObjectType, Equals, ct.EventTypeApp)
		c.Assert(e.ObjectID, Equals, app.ID)
		c.Assert(eventApp, NotNil)
		c.Assert(eventApp.ID, Equals, app.ID)
		return eventApp
	}
	assertReleaseEvent := func(e *ct.Event, id string) {
		var eventRelease *ct.Release
		c.Assert(json.Unmarshal(e.Data, &eventRelease), IsNil)
		c.Assert(e.ObjectType, Equals, ct.EventTypeRelease)
		c.Assert(e.ObjectID, Equals, id)
		c.Assert(eventRelease, NotNil)
		c.Assert(eventRelease.ID, Equals, id)
		c.Assert(eventRelease.AppID, Equals, app.ID)
	}

	eventAssertions := []func(*ct.Event){
		func(e *ct.Event) {
			a := assertAppEvent(e)
			c.Assert(a.ReleaseID, Equals, app.ReleaseID)
			c.Assert(a.Strategy, Equals, app.Strategy)
			c.Assert(a.Meta, DeepEquals, app.Meta)
		},
		func(e *ct.Event) {
			assertReleaseEvent(e, release.ID)
		},
		func(e *ct.Event) {
			assertReleaseEvent(e, nextRelease.ID)
		},
		func(e *ct.Event) {
			var eventRelease *ct.AppRelease
			c.Assert(json.Unmarshal(e.Data, &eventRelease), IsNil)
			c.Assert(e.ObjectType, Equals, ct.EventTypeAppRelease)
			c.Assert(e.ObjectID, Equals, release.ID)
			c.Assert(eventRelease, NotNil)
			c.Assert(eventRelease.Release, NotNil)
			c.Assert(eventRelease.Release.ID, Equals, release.ID)
			c.Assert(eventRelease.PrevRelease, IsNil)
		},
		func(e *ct.Event) {
			a := assertAppEvent(e)
			c.Assert(a.Strategy, Equals, newStrategy)
			c.Assert(a.Meta, DeepEquals, app.Meta)
		},
		func(e *ct.Event) {
			a := assertAppEvent(e)
			c.Assert(a.Strategy, Equals, newStrategy)
			c.Assert(a.Meta, DeepEquals, newMeta)
		},
		func(e *ct.Event) {
			var eventRelease *ct.AppRelease
			c.Assert(json.Unmarshal(e.Data, &eventRelease), IsNil)
			c.Assert(e.ObjectType, Equals, ct.EventTypeAppRelease)
			c.Assert(e.ObjectID, Equals, nextRelease.ID)
			c.Assert(eventRelease, NotNil)
			c.Assert(eventRelease.Release, NotNil)
			c.Assert(eventRelease.Release.ID, Equals, nextRelease.ID)
			c.Assert(eventRelease.PrevRelease, NotNil)
			c.Assert(eventRelease.PrevRelease.ID, Equals, release.ID)
		},
	}

outer:
	for i, fn := range eventAssertions {
	inner:
		for {
			select {
			case e, ok := <-events:
				if !ok {
					c.Fatal("unexpected close of event stream")
				}
				// ignore events for other apps
				if e.AppID != app.ID {
					continue inner
				}
				fn(e)
				continue outer
			case <-time.After(10 * time.Second):
				c.Fatalf("Timed out waiting for event %d", i)
			}
		}
	}
}

func (s *S) TestStreamReleaseEvents(c *C) {
	app := s.createTestApp(c, &ct.App{})

	events := make(chan *ct.Event)
	stream, err := s.c.StreamEvents(ct.StreamEventsOptions{}, events)
	c.Assert(err, IsNil)
	defer stream.Close()

	release := s.createTestRelease(c, app.ID, &ct.Release{})

	var gotRelease, gotArtifact bool
	for i := 0; i < 2; i++ {
		select {
		case e, ok := <-events:
			if !ok {
				c.Fatal("unexpected close of event stream")
			}
			switch e.ObjectType {
			case ct.EventTypeArtifact:
				var eventArtifact *ct.Artifact
				c.Assert(json.Unmarshal(e.Data, &eventArtifact), IsNil)
				c.Assert(e.ObjectID, Equals, release.ArtifactIDs[0])
				c.Assert(eventArtifact, NotNil)
				c.Assert(eventArtifact.ID, Equals, release.ArtifactIDs[0])
				gotArtifact = true
			case ct.EventTypeRelease:
				var eventRelease *ct.Release
				c.Assert(json.Unmarshal(e.Data, &eventRelease), IsNil)
				c.Assert(e.AppID, Equals, app.ID)
				c.Assert(e.ObjectID, Equals, release.ID)
				c.Assert(eventRelease, DeepEquals, release)
				gotRelease = true
			case ct.EventTypeApp:
			default:
				c.Errorf("unexpected event object %s", e.ObjectType)
			}
		case <-time.After(10 * time.Second):
			c.Fatalf("Timed out waiting for event %d", i)
		}
	}

	c.Assert(gotArtifact, Equals, true)
	c.Assert(gotRelease, Equals, true)
}

func (s *S) TestStreamFormationEvents(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "stream-formation-test"})
	release := s.createTestRelease(c, app.ID, &ct.Release{
		Processes: map[string]ct.ProcessType{"foo": {}},
	})

	events := make(chan *ct.Event)
	stream, err := s.c.StreamEvents(ct.StreamEventsOptions{
		ObjectTypes: []ct.EventType{ct.EventTypeScale},
	}, events)
	c.Assert(err, IsNil)
	defer stream.Close()

	formation := s.createTestFormation(c, &ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"foo": 1},
	})
	defer s.deleteTestFormation(formation)

	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		var scale *ct.Scale
		c.Assert(json.Unmarshal(e.Data, &scale), IsNil)
		c.Assert(e.AppID, Equals, app.ID)
		c.Assert(e.ObjectType, Equals, ct.EventTypeScale)
		c.Assert(e.ObjectID, Equals, strings.Join([]string{app.ID, release.ID}, ":"))
		c.Assert(scale, NotNil)
		c.Assert(scale.Processes, DeepEquals, formation.Processes)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for scale event")
	}

	nextFormation := s.createTestFormation(c, &ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"foo": 2},
	})
	defer s.deleteTestFormation(nextFormation)

	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		var scale *ct.Scale
		c.Assert(json.Unmarshal(e.Data, &scale), IsNil)
		c.Assert(e.AppID, Equals, app.ID)
		c.Assert(e.ObjectType, Equals, ct.EventTypeScale)
		c.Assert(e.ObjectID, Equals, strings.Join([]string{app.ID, release.ID}, ":"))
		c.Assert(scale, NotNil)
		c.Assert(scale.Processes, DeepEquals, nextFormation.Processes)
		c.Assert(scale.PrevProcesses, DeepEquals, formation.Processes)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for scale event")
	}

	c.Assert(s.c.DeleteFormation(app.ID, release.ID), IsNil)

	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		var scale *ct.Scale
		c.Assert(json.Unmarshal(e.Data, &scale), IsNil)
		c.Assert(e.AppID, Equals, app.ID)
		c.Assert(e.ObjectType, Equals, ct.EventTypeScale)
		c.Assert(e.ObjectID, Equals, strings.Join([]string{app.ID, release.ID}, ":"))
		c.Assert(scale, NotNil)
		c.Assert(scale.Processes, IsNil)
		c.Assert(scale.PrevProcesses, DeepEquals, nextFormation.Processes)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for scale event")
	}
}

func (s *S) TestStreamProviderEvents(c *C) {
	events := make(chan *ct.Event)
	stream, err := s.c.StreamEvents(ct.StreamEventsOptions{}, events)
	c.Assert(err, IsNil)
	defer stream.Close()

	provider := s.createTestProvider(c, &ct.Provider{
		URL:  "https://test-stream-provider.example.com",
		Name: "test-stream-provider",
	})

	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		var eventProvider *ct.Provider
		c.Assert(json.Unmarshal(e.Data, &eventProvider), IsNil)
		c.Assert(e.AppID, Equals, "")
		c.Assert(e.ObjectType, Equals, ct.EventTypeProvider)
		c.Assert(e.ObjectID, Equals, provider.ID)
		c.Assert(eventProvider, DeepEquals, provider)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for provider event")
	}
}

func (s *S) TestStreamResourceEvents(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "app4"})

	events := make(chan *ct.Event)
	stream, err := s.c.StreamEvents(ct.StreamEventsOptions{
		ObjectTypes: []ct.EventType{
			ct.EventTypeResource,
			ct.EventTypeResourceDeletion,
		},
	}, events)
	c.Assert(err, IsNil)
	defer stream.Close()

	resource, provider, srv := s.provisionTestResourceWithServer(c, "stream-resources", []string{app.ID})
	defer srv.Close()

	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		var eventResource *ct.Resource
		c.Assert(json.Unmarshal(e.Data, &eventResource), IsNil)
		c.Assert(e.AppID, Equals, app.ID)
		c.Assert(e.ObjectType, Equals, ct.EventTypeResource)
		c.Assert(e.ObjectID, Equals, resource.ID)
		c.Assert(eventResource, DeepEquals, resource)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for resource event")
	}

	_, err = s.c.DeleteResource(provider.ID, resource.ID)
	c.Assert(err, IsNil)

	select {
	case e, ok := <-events:
		if !ok {
			c.Fatal("unexpected close of event stream")
		}
		var eventResource *ct.Resource
		c.Assert(json.Unmarshal(e.Data, &eventResource), IsNil)
		c.Assert(e.AppID, Equals, app.ID)
		c.Assert(e.ObjectType, Equals, ct.EventTypeResourceDeletion)
		c.Assert(e.ObjectID, Equals, resource.ID)
		c.Assert(eventResource, DeepEquals, resource)
	case <-time.After(10 * time.Second):
		c.Fatal("Timed out waiting for resource_deletion event")
	}
}

func (s *S) TestListEvents(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "app5"})
	release := s.createTestRelease(c, app.ID, &ct.Release{})

	c.Assert(s.c.SetAppRelease(app.ID, release.ID), IsNil)
	newStrategy := "one-by-one"
	c.Assert(s.c.UpdateApp(&ct.App{
		ID:       app.ID,
		Strategy: newStrategy,
	}), IsNil)
	newMeta := map[string]string{
		"foo": "bar",
	}
	c.Assert(s.c.UpdateApp(&ct.App{
		ID:   app.ID,
		Meta: newMeta,
	}), IsNil)

	events, err := s.c.ListEvents(ct.ListEventsOptions{
		ObjectTypes: []ct.EventType{ct.EventTypeApp, ct.EventTypeRelease, ct.EventTypeAppRelease},
		AppID:       app.ID,
	})
	c.Assert(err, IsNil)

	assertAppEvent := func(e *ct.Event) *ct.App {
		var eventApp *ct.App
		c.Assert(json.Unmarshal(e.Data, &eventApp), IsNil)
		c.Assert(e.AppID, Equals, app.ID)
		c.Assert(e.ObjectType, Equals, ct.EventTypeApp)
		c.Assert(e.ObjectID, Equals, app.ID)
		c.Assert(eventApp, NotNil)
		c.Assert(eventApp.ID, Equals, app.ID)
		return eventApp
	}

	eventAssertions := []func(*ct.Event){
		func(e *ct.Event) {
			a := assertAppEvent(e)
			c.Assert(a.ReleaseID, Equals, app.ReleaseID)
			c.Assert(a.Strategy, Equals, app.Strategy)
			c.Assert(a.Meta, DeepEquals, app.Meta)
		},
		func(e *ct.Event) {
			var eventRelease *ct.Release
			c.Assert(json.Unmarshal(e.Data, &eventRelease), IsNil)
			c.Assert(e.AppID, Equals, app.ID)
			c.Assert(e.ObjectType, Equals, ct.EventTypeRelease)
			c.Assert(e.ObjectID, Equals, release.ID)
			c.Assert(eventRelease, NotNil)
			c.Assert(eventRelease.ID, Equals, release.ID)
			c.Assert(eventRelease.AppID, Equals, app.ID)
		},
		func(e *ct.Event) {
			var eventRelease *ct.AppRelease
			c.Assert(json.Unmarshal(e.Data, &eventRelease), IsNil)
			c.Assert(e.AppID, Equals, app.ID)
			c.Assert(e.ObjectType, Equals, ct.EventTypeAppRelease)
			c.Assert(e.ObjectID, Equals, release.ID)
			c.Assert(eventRelease, NotNil)
			c.Assert(eventRelease.Release, NotNil)
			c.Assert(eventRelease.Release.ID, Equals, release.ID)
		},
		func(e *ct.Event) {
			a := assertAppEvent(e)
			c.Assert(a.Strategy, Equals, newStrategy)
			c.Assert(a.Meta, DeepEquals, app.Meta)
		},
		func(e *ct.Event) {
			a := assertAppEvent(e)
			c.Assert(a.Strategy, Equals, newStrategy)
			c.Assert(a.Meta, DeepEquals, newMeta)
		},
	}

	c.Assert(len(events), Equals, len(eventAssertions))
	eventsLen := len(events)
	for i, fn := range eventAssertions {
		fn(events[eventsLen-i-1])
	}

	eventsSlice, err := s.c.ListEvents(ct.ListEventsOptions{
		ObjectTypes: []ct.EventType{ct.EventTypeApp, ct.EventTypeAppRelease},
		BeforeID:    &events[0].ID,
		SinceID:     &events[eventsLen-1].ID,
	})
	c.Assert(err, IsNil)
	c.Assert(len(eventsSlice), Equals, 2)
	c.Assert(eventsSlice[0].ID, Equals, events[1].ID)
	c.Assert(eventsSlice[1].ID, Equals, events[2].ID)

	eventsSlice, err = s.c.ListEvents(ct.ListEventsOptions{
		ObjectTypes: []ct.EventType{ct.EventTypeApp, ct.EventTypeAppRelease},
		BeforeID:    &events[0].ID,
		SinceID:     &events[eventsLen-1].ID,
		Count:       1,
	})
	c.Assert(err, IsNil)
	c.Assert(len(eventsSlice), Equals, 1)
	c.Assert(eventsSlice[0].ID, Equals, events[1].ID)
}

func (s *S) TestGetEvent(c *C) {
	// ensure there's at least one event
	_ = s.createTestRelease(c, "", &ct.Release{})

	events, err := s.c.ListEvents(ct.ListEventsOptions{})
	c.Assert(err, IsNil)
	c.Assert(len(events), Not(Equals), 0)

	event, err := s.c.GetEvent(events[0].ID)
	c.Assert(err, IsNil)
	c.Assert(event, DeepEquals, events[0])
}
