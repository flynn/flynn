package main

import (
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
)

type HealthcheckSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&HealthcheckSuite{})

func (s *HealthcheckSuite) createAppWithService(t *c.C, process string, service *host.Service) (*ct.App, *ct.Release) {
	app, release := s.createApp(t)

	release.ID = ""
	proctype := release.Processes[process]
	proctype.Ports = []ct.Port{{
		Proto:   "tcp",
		Service: service,
	}}
	release.Processes[process] = proctype

	t.Assert(s.controllerClient(t).CreateRelease(release), c.IsNil)
	t.Assert(s.controllerClient(t).SetAppRelease(app.ID, release.ID), c.IsNil)
	return app, release
}

func (s *HealthcheckSuite) TestChecker(t *c.C) {
	// start app with ping service, register with checker
	app, _ := s.createAppWithService(t, "ping", &host.Service{
		Name:   "ping-checker",
		Create: true,
		Check:  &host.HealthCheck{Type: "tcp"},
	})
	t.Assert(flynn(t, "/", "-a", app.Name, "scale", "ping=1"), Succeeds)
	_, err := s.discoverdClient(t).Instances("ping-checker", 10*time.Second)
	t.Assert(err, c.IsNil)
}

func (s *HealthcheckSuite) TestWithoutChecker(t *c.C) {
	// start app with a service but no checker
	app, _ := s.createAppWithService(t, "ping", &host.Service{
		Name:   "ping-without-checker",
		Create: true,
	})
	t.Assert(flynn(t, "/", "-a", app.Name, "scale", "ping=1"), Succeeds)
	// make sure app is registered and unregistered when the process terminates
	_, err := s.discoverdClient(t).Instances("ping-without-checker", 3*time.Second)
	t.Assert(err, c.IsNil)

	events := make(chan *discoverd.Event)
	stream, err := s.discoverdClient(t).Service("ping-without-checker").Watch(events)
	defer stream.Close()
	t.Assert(err, c.IsNil)

	t.Assert(flynn(t, "/", "-a", app.Name, "scale", "ping=0"), Succeeds)

outer:
	for {
		select {
		case e := <-events:
			if e.Kind != discoverd.EventKindDown {
				continue
			}
			break outer
		case <-time.After(time.Second * 30):
			t.Error("Timed out waiting for a down event!")
		}
	}
}

func (s *HealthcheckSuite) TestFailure(t *c.C) {
	// start an app that is failing checks
	app, _ := s.createAppWithService(t, "printer", &host.Service{
		Name:   "healthcheck-failure",
		Create: true,
		Check:  &host.HealthCheck{Type: "tcp"},
	})
	t.Assert(flynn(t, "/", "-a", app.Name, "scale", "printer=1"), Succeeds)
	// confirm that it's never registered
	_, err := s.discoverdClient(t).Instances("healthcheck-failure", 5*time.Second)
	t.Assert(err, c.NotNil)
}

func (s *HealthcheckSuite) TestKillDown(t *c.C) {
	// start an app that is failing checks /w killdown
	app, _ := s.createAppWithService(t, "printer", &host.Service{
		Name:   "healthcheck-killdown",
		Create: true,
		Check:  &host.HealthCheck{Type: "tcp", KillDown: true, StartTimeout: 2 * time.Second},
	})
	events := make(chan *ct.JobEvent)
	stream, err := s.controllerClient(t).StreamJobEvents(app.ID, 0, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	t.Assert(flynn(t, "/", "-a", app.Name, "scale", "printer=1"), Succeeds)
	// make sure we get a killdown event in the first 10-30s and the job marked
	// as failed
	waitForJobEvents(t, stream, events, jobEvents{"printer": {"down": 1}})
}
