package main

import (
	"time"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
	. "github.com/titanous/gocheck"
)

func (s *S) TestFormationStreaming(c *C) {
	before := time.Now()
	release := s.createTestRelease(c, &ct.Release{})
	app := s.createTestApp(c, &ct.App{Name: "streamtest-existing"})
	s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})

	client, err := controller.NewClient(s.srv.URL, authKey)
	c.Assert(err, IsNil)

	updates, streamErr := client.StreamFormations(&before)

	var existingFound bool
	for f := range updates.Chan {
		if f.App == nil {
			break
		}
		if f.Release.ID == release.ID {
			existingFound = true
		}
	}
	c.Assert(existingFound, Equals, true)
	c.Assert(*streamErr, IsNil)

	release = s.createTestRelease(c, &ct.Release{})
	app = s.createTestApp(c, &ct.App{Name: "streamtest"})
	formation := s.createTestFormation(c, &ct.Formation{
		ReleaseID: release.ID,
		AppID:     app.ID,
		Processes: map[string]int{"foo": 1},
	})

	var out *ct.ExpandedFormation
	select {
	case out = <-updates.Chan:
	case <-time.After(time.Second):
		c.Fatal("timed out waiting for create")
	}
	c.Assert(out.Release, DeepEquals, release)
	c.Assert(out.App, DeepEquals, app)
	c.Assert(out.Processes, DeepEquals, formation.Processes)
	c.Assert(out.Artifact.CreatedAt, Not(IsNil))
	c.Assert(out.Artifact.ID, Equals, release.ArtifactID)

	s.Delete(formationPath(app.ID, release.ID))

	select {
	case out = <-updates.Chan:
	case <-time.After(time.Second):
		c.Fatal("timed out waiting for delete")
	}
	c.Assert(out.Release, DeepEquals, release)
	c.Assert(out.App, DeepEquals, app)
	c.Assert(out.Processes, IsNil)

	client.Close()
}
