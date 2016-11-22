package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/dialer"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/router/types"
	c "github.com/flynn/go-check"
)

type DomainMigrationSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&DomainMigrationSuite{})

func (s *DomainMigrationSuite) migrateDomain(t *c.C, client controller.Client, dm *ct.DomainMigration) *ct.DomainMigration {
	debugf(t, "migrating domain from %s to %s", dm.OldDomain, dm.Domain)

	events := make(chan *ct.Event)
	stream, err := client.StreamEvents(ct.StreamEventsOptions{
		ObjectTypes: []ct.EventType{ct.EventTypeDomainMigration},
	}, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	prevRouterRelease, err := client.GetAppRelease("router")
	t.Assert(err, c.IsNil)

	err = client.PutDomain(dm)
	t.Assert(err, c.IsNil)

	waitEvent := func(typ string, timeout time.Duration) (event ct.DomainMigrationEvent) {
		debugf(t, "waiting for %s domain migration event", typ)
		var e *ct.Event
		var ok bool
		select {
		case e, ok = <-events:
			if !ok {
				t.Fatalf("event stream closed unexpectedly: %s", stream.Err())
			}
			debugf(t, "got %s domain migration event", typ)
		case <-time.After(timeout):
			t.Fatalf("timed out waiting for %s domain migration event", typ)
		}
		t.Assert(e.Data, c.NotNil)
		t.Assert(json.Unmarshal(e.Data, &event), c.IsNil)
		return
	}

	// created
	event := waitEvent("initial", 2*time.Minute)
	t.Assert(event.Error, c.Equals, "")
	t.Assert(event.DomainMigration, c.NotNil)
	t.Assert(event.DomainMigration.ID, c.Equals, dm.ID)
	t.Assert(event.DomainMigration.OldDomain, c.Equals, dm.OldDomain)
	t.Assert(event.DomainMigration.Domain, c.Equals, dm.Domain)
	t.Assert(event.DomainMigration.OldTLSCert, c.NotNil)
	t.Assert(event.DomainMigration.CreatedAt, c.NotNil)
	t.Assert(event.DomainMigration.CreatedAt.Equal(*dm.CreatedAt), c.Equals, true)
	t.Assert(event.DomainMigration.FinishedAt, c.IsNil)

	// complete
	event = waitEvent("final", 3*time.Minute)
	t.Assert(event.Error, c.Equals, "")
	t.Assert(event.DomainMigration, c.NotNil)
	t.Assert(event.DomainMigration.ID, c.Equals, dm.ID)
	t.Assert(event.DomainMigration.OldDomain, c.Equals, dm.OldDomain)
	t.Assert(event.DomainMigration.Domain, c.Equals, dm.Domain)
	t.Assert(event.DomainMigration.TLSCert, c.NotNil)
	t.Assert(event.DomainMigration.OldTLSCert, c.NotNil)
	t.Assert(event.DomainMigration.CreatedAt, c.NotNil)
	t.Assert(event.DomainMigration.CreatedAt.Equal(*dm.CreatedAt), c.Equals, true)
	t.Assert(event.DomainMigration.FinishedAt, c.NotNil)

	cert := event.DomainMigration.TLSCert

	controllerRelease, err := client.GetAppRelease("controller")
	t.Assert(err, c.IsNil)
	t.Assert(controllerRelease.Env["DEFAULT_ROUTE_DOMAIN"], c.Equals, dm.Domain)
	t.Assert(controllerRelease.Env["CA_CERT"], c.Equals, cert.CACert)

	routerRelease, err := client.GetAppRelease("router")
	t.Assert(err, c.IsNil)
	t.Assert(routerRelease.Env["TLSCERT"], c.Equals, cert.Cert)
	t.Assert(routerRelease.Env["TLSKEY"], c.Not(c.Equals), "")
	t.Assert(routerRelease.Env["TLSKEY"], c.Not(c.Equals), prevRouterRelease.Env["TLSKEY"])

	dashboardRelease, err := client.GetAppRelease("dashboard")
	t.Assert(err, c.IsNil)
	t.Assert(dashboardRelease.Env["DEFAULT_ROUTE_DOMAIN"], c.Equals, dm.Domain)
	t.Assert(dashboardRelease.Env["CONTROLLER_DOMAIN"], c.Equals, fmt.Sprintf("controller.%s", dm.Domain))
	t.Assert(dashboardRelease.Env["URL"], c.Equals, fmt.Sprintf("https://dashboard.%s", dm.Domain))
	t.Assert(dashboardRelease.Env["CA_CERT"], c.Equals, cert.CACert)

	routes, err := client.RouteList("controller")
	t.Assert(err, c.IsNil)
	t.Assert(len(routes), c.Equals, 2) // one for both new and old domain
	var route *router.Route
	for _, r := range routes {
		if strings.HasSuffix(r.Domain, dm.Domain) {
			route = r
			break
		}
	}
	t.Assert(route, c.Not(c.IsNil))
	t.Assert(route.Domain, c.Equals, fmt.Sprintf("controller.%s", dm.Domain))
	t.Assert(route.Certificate.Cert, c.Equals, strings.TrimSuffix(cert.Cert, "\n"))

	var doPing func(string, int)
	doPing = func(component string, retriesRemaining int) {
		url := fmt.Sprintf("http://%s.%s/ping", component, dm.Domain)
		httpClient := &http.Client{Transport: &http.Transport{Dial: dialer.Retry.Dial}}
		res, err := httpClient.Get(url)
		if (err != nil || res.StatusCode != 200) && retriesRemaining > 0 {
			time.Sleep(100 * time.Millisecond)
			doPing(component, retriesRemaining-1)
			return
		}
		t.Assert(err, c.IsNil)
		t.Assert(res.StatusCode, c.Equals, 200, c.Commentf("failed to ping %s", component))
	}
	doPing("controller", 3)
	doPing("dashboard", 3)

	return event.DomainMigration
}

func (s *DomainMigrationSuite) TestDomainMigration(t *c.C) {
	x := s.bootCluster(t, 1)
	defer x.Destroy()

	cc := x.controller
	release, err := cc.GetAppRelease("controller")
	t.Assert(err, c.IsNil)
	oldDomain := release.Env["DEFAULT_ROUTE_DOMAIN"]

	// create app
	app, _ := s.createAppWithClient(t, cc)
	appRoutes, err := cc.RouteList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(len(appRoutes), c.Equals, 1)

	newDomain := random.String(32) + ".local"
	Hostnames.Add(t, x.IP, "controller."+newDomain, "dashboard."+newDomain)
	dm := &ct.DomainMigration{
		OldDomain: oldDomain,
		Domain:    newDomain,
	}
	dm = s.migrateDomain(t, cc, dm)

	// make sure a new route was created for the app
	appRoutes, err = cc.RouteList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(len(appRoutes), c.Equals, 2)
	t.Assert(strings.HasSuffix(appRoutes[0].Domain, dm.Domain), c.Equals, true)
	t.Assert(strings.HasSuffix(appRoutes[1].Domain, dm.OldDomain), c.Equals, true)

	s.migrateDomain(t, cc, &ct.DomainMigration{
		OldDomain: dm.Domain,
		Domain:    dm.OldDomain,
		TLSCert:   dm.OldTLSCert,
	})

	// app should still only have the two routes
	appRoutes, err = cc.RouteList(app.ID)
	t.Assert(err, c.IsNil)
	t.Assert(len(appRoutes), c.Equals, 2)
}
