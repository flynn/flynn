package main

import (
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
)

func (s *S) TestFormationStreaming(c *C) {
	before := time.Now()
	release := s.createTestRelease(c, &ct.Release{})
	app := s.createTestApp(c, &ct.App{Name: "streamtest-existing"})
	s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})

	updates := make(chan *ct.ExpandedFormation)
	streamCtrl, connectErr := s.c.StreamFormations(&before, updates)

	c.Assert(connectErr, IsNil)
	var existingFound bool
	for f := range updates {
		if f.App == nil {
			break
		}
		if f.Release.ID == release.ID {
			existingFound = true
		}
	}
	c.Assert(streamCtrl.Err(), IsNil)
	c.Assert(existingFound, Equals, true)

	release = s.createTestRelease(c, &ct.Release{})
	app = s.createTestApp(c, &ct.App{Name: "streamtest"})
	formation := s.createTestFormation(c, &ct.Formation{
		ReleaseID: release.ID,
		AppID:     app.ID,
		Processes: map[string]int{"foo": 1},
	})

	var out *ct.ExpandedFormation
	select {
	case out = <-updates:
	case <-time.After(time.Second):
		c.Fatal("timed out waiting for create")
	}
	c.Assert(streamCtrl.Err(), IsNil)
	c.Assert(out.Release, DeepEquals, release)
	c.Assert(out.App, DeepEquals, app)
	c.Assert(out.Processes, DeepEquals, formation.Processes)
	c.Assert(out.Artifact.CreatedAt, Not(IsNil))
	c.Assert(out.Artifact.ID, Equals, release.ArtifactID)

	c.Assert(s.c.DeleteFormation(app.ID, release.ID), IsNil)

	select {
	case out = <-updates:
	case <-time.After(time.Second):
		c.Fatal("timed out waiting for delete")
	}
	c.Assert(streamCtrl.Err(), IsNil)
	c.Assert(out.Release, DeepEquals, release)
	c.Assert(out.App, DeepEquals, app)
	c.Assert(out.Processes, IsNil)
}
