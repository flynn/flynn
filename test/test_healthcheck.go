package main

import (
	"encoding/json"
	"net/http"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/status"
	c "github.com/flynn/go-check"
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

	t.Assert(s.controllerClient(t).CreateRelease(app.ID, release), c.IsNil)
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
	app, release := s.createAppWithService(t, "printer", &host.Service{
		Name:   "healthcheck-killdown",
		Create: true,
		Check:  &host.HealthCheck{Type: "tcp", KillDown: true, StartTimeout: 2 * time.Second},
	})
	watcher, err := s.controllerClient(t).WatchJobEvents(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	t.Assert(flynn(t, "/", "-a", app.Name, "scale", "printer=1"), Succeeds)
	// make sure we get a killdown event in the first 10-30s and the job marked
	// as failed
	err = watcher.WaitFor(ct.JobEvents{"printer": {"down": 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)
}

func (s *HealthcheckSuite) TestStatus(t *c.C) {
	routes, err := s.controllerClient(t).RouteList("status")
	t.Assert(err, c.IsNil)
	t.Assert(routes, c.HasLen, 1)

	req, _ := http.NewRequest("GET", "http://"+routerIP, nil)
	req.Host = routes[0].HTTPRoute().Domain
	res, err := http.DefaultClient.Do(req)
	t.Assert(err, c.IsNil)
	defer res.Body.Close()

	var data struct {
		Data struct {
			Status status.Code
			Detail map[string]status.Status
		}
	}
	err = json.NewDecoder(res.Body).Decode(&data)
	t.Assert(err, c.IsNil)

	t.Assert(data.Data.Status, c.Equals, status.CodeHealthy)
	t.Assert(data.Data.Detail, c.HasLen, 14)
	optional := map[string]bool{"mariadb": true, "mongodb": true}
	for name, s := range data.Data.Detail {
		if !optional[name] {
			t.Assert(s.Status, c.Equals, status.CodeHealthy, c.Commentf("name = %s", name))
		}
	}
}
