package main

import (
	"time"

	ct "github.com/flynn/flynn/controller/types"
	. "github.com/flynn/go-check"
)

func (s *S) TestFormationStreaming(c *C) {
	before := time.Now()
	app := s.createTestApp(c, &ct.App{Name: "streamtest-existing"})
	release := s.createTestRelease(c, app.ID, &ct.Release{})
	s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})

	updates := make(chan *ct.ExpandedFormation)
	stream, err := s.c.StreamFormations(&before, updates)
	c.Assert(err, IsNil)
	defer stream.Close()

	nextUpdate := func() *ct.ExpandedFormation {
		for {
			select {
			case f, ok := <-updates:
				if !ok {
					c.Fatalf("formation stream closed: %s", stream.Err())
				}
				if f.Release == nil || f.Release.AppID != app.ID {
					continue
				}
				return f
			case <-time.After(10 * time.Second):
				c.Fatal("timed out waiting for formation update")
			}
		}
	}

	update := nextUpdate()
	c.Assert(update.App, DeepEquals, app)
	c.Assert(update.Release, DeepEquals, release)

	app = s.createTestApp(c, &ct.App{Name: "streamtest"})
	release = s.createTestRelease(c, app.ID, &ct.Release{
		Processes: map[string]ct.ProcessType{"foo": {}},
	})
	formation := s.createTestFormation(c, &ct.Formation{
		ReleaseID: release.ID,
		AppID:     app.ID,
		Processes: map[string]int{"foo": 1},
	})

	update = nextUpdate()
	c.Assert(update.Release, DeepEquals, release)
	c.Assert(update.App, DeepEquals, app)
	c.Assert(update.Artifacts, HasLen, len(release.ArtifactIDs))
	for i, id := range release.ArtifactIDs {
		c.Assert(update.Artifacts[i].ID, Equals, id)
	}
	c.Assert(update.Processes, DeepEquals, formation.Processes)

	c.Assert(s.c.DeleteFormation(app.ID, release.ID), IsNil)

	update = nextUpdate()
	c.Assert(update.Release, DeepEquals, release)
	c.Assert(update.App, DeepEquals, app)
	c.Assert(update.Processes, IsNil)
}

func (s *S) TestFormationStreamDeleted(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "formation-stream-deleted"})

	// create 3 releases with formations
	releases := make([]*ct.Release, 3)
	for i := 0; i < 3; i++ {
		releases[i] = s.createTestRelease(c, app.ID, &ct.Release{})
		s.createTestFormation(c, &ct.Formation{ReleaseID: releases[i].ID, AppID: app.ID})
	}

	// delete the first release (which also deletes the first formation)
	_, err := s.c.DeleteRelease(app.ID, releases[0].ID)
	c.Assert(err, IsNil)
	deletedRelease := releases[0]

	// check streaming formations does not include the deleted release
	updates := make(chan *ct.ExpandedFormation)
	stream, err := s.c.StreamFormations(nil, updates)
	c.Assert(err, IsNil)
	defer stream.Close()

	actual := 0
outer:
	for {
		select {
		case update, ok := <-updates:
			if !ok {
				c.Fatalf("stream closed unexpectedly: %s", stream.Err())
			}
			if update.App == nil {
				break outer
			}
			if update.App.ID != app.ID {
				continue
			}
			if update.Release != nil && update.Release.ID == deletedRelease.ID {
				c.Fatal("expected stream to not include deleted release but it did")
			}
			actual++
		case <-time.After(10 * time.Second):
			c.Fatal("timed out waiting for formation updates")
		}
	}
	expected := len(releases) - 1
	if actual != expected {
		c.Fatalf("expected %d updates, got %d", expected, actual)
	}
}

func (s *S) TestFormationListActive(c *C) {
	app1 := s.createTestApp(c, &ct.App{})
	app2 := s.createTestApp(c, &ct.App{})
	artifacts := []*ct.Artifact{
		s.createTestArtifact(c, &ct.Artifact{}),
		s.createTestArtifact(c, &ct.Artifact{}),
	}

	createFormation := func(app *ct.App, procs map[string]int) *ct.ExpandedFormation {
		release := &ct.Release{
			ArtifactIDs: []string{artifacts[0].ID, artifacts[1].ID},
			Processes:   make(map[string]ct.ProcessType, len(procs)),
		}
		for typ := range procs {
			release.Processes[typ] = ct.ProcessType{}
		}
		s.createTestRelease(c, app.ID, release)
		s.createTestFormation(c, &ct.Formation{
			AppID:     app.ID,
			ReleaseID: release.ID,
			Processes: procs,
		})
		return &ct.ExpandedFormation{
			App:       app,
			Release:   release,
			Artifacts: artifacts,
			Processes: procs,
		}
	}

	formations := []*ct.ExpandedFormation{
		createFormation(app1, map[string]int{"web": 0}),
		createFormation(app1, map[string]int{"web": 1}),
		createFormation(app2, map[string]int{"web": 0, "worker": 0}),
		createFormation(app2, map[string]int{"web": 0, "worker": 1}),
		createFormation(app2, map[string]int{"web": 1, "worker": 2}),
	}

	list, err := s.c.FormationListActive()
	c.Assert(err, IsNil)
	c.Assert(list, HasLen, 3)

	// check that we only get the formations with a non-zero process count,
	// most recently updated first
	expected := []*ct.ExpandedFormation{formations[4], formations[3], formations[1]}
	for i, f := range expected {
		actual := list[i]
		c.Assert(actual.App.ID, Equals, f.App.ID)
		c.Assert(actual.Release.ID, Equals, f.Release.ID)
		c.Assert(actual.Artifacts, DeepEquals, f.Artifacts)
		c.Assert(actual.Processes, DeepEquals, f.Processes)
	}
}
