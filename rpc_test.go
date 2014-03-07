package main

import (
	"time"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/rpcplus"
	. "launchpad.net/gocheck"
)

func (s *S) TestFormationStreaming(c *C) {
	client, err := rpcplus.DialHTTP("tcp", s.srv.URL[7:])
	c.Assert(err, IsNil)
	ch := make(chan *ct.ExpandedFormation)

	client.StreamGo("Controller.StreamFormations", struct{}{}, ch)

	select {
	case <-ch:
	case <-time.After(time.Second):
		c.Fatal("timed out waiting for sentinel")
	}

	release := s.createTestRelease(c, &ct.Release{})
	app := s.createTestApp(c, &ct.App{Name: "streamtest"})
	formation := s.createTestFormation(c, &ct.Formation{
		ReleaseID: release.ID,
		AppID:     app.ID,
		Processes: map[string]int{"foo": 1},
	})

	var out *ct.ExpandedFormation
	select {
	case out = <-ch:
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
	case out = <-ch:
	case <-time.After(time.Second):
		c.Fatal("timed out waiting for delete")
	}
	c.Assert(out.Release, DeepEquals, release)
	c.Assert(out.App, DeepEquals, app)
	c.Assert(out.Processes, IsNil)

	client.Close()
}
