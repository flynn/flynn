package main

import (
	"github.com/flynn/rpcplus"
	. "launchpad.net/gocheck"
)

func (s *S) TestFormationStreaming(c *C) {
	client, err := rpcplus.DialHTTP("tcp", s.srv.URL[7:])
	c.Assert(err, IsNil)
	ch := make(chan *ExpandedFormation)

	client.StreamGo("Controller.StreamFormations", struct{}{}, ch)

	release := s.createTestRelease(c, &Release{})
	app := s.createTestApp(c, &App{Name: "streamtest"})
	formation := s.createTestFormation(c, &Formation{
		ReleaseID: release.ID,
		AppID:     app.ID,
		Processes: map[string]int{"foo": 1},
	})

	// create event
	out := <-ch
	c.Assert(out.Release, DeepEquals, release)
	c.Assert(out.App, DeepEquals, app)
	c.Assert(out.Processes, DeepEquals, formation.Processes)

	s.Delete(formationPath(app.ID, release.ID))

	// delete event
	out = <-ch
	c.Assert(out.Release, DeepEquals, release)
	c.Assert(out.App, DeepEquals, app)
	c.Assert(out.Processes, IsNil)

	client.Close()
}
