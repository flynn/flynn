package domain_migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/worker/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/tlscert"
	routerc "github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
	"github.com/flynn/que-go"
	"github.com/inconshreveable/log15"
)

// limits the number of concurrent requests to the router
const maxActiveRouteUpdates = 5

type context struct {
	db     *postgres.DB
	client controller.Client
	logger log15.Logger
}

type migration struct {
	db                 *postgres.DB
	client             controller.Client
	rc                 routerc.Client
	logger             log15.Logger
	dm                 *ct.DomainMigration
	activeRouteUpdates chan struct{}
	stop               chan struct{}
}

func JobHandler(db *postgres.DB, client controller.Client, logger log15.Logger) func(*que.Job) error {
	return (&context{db, client, logger}).HandleDomainMigration
}

func (m *migration) cancelDeploy() <-chan struct{} {
	ch := make(chan struct{})
	timeout := time.After(5 * time.Minute)
	go func() {
		defer close(ch)
		select {
		case <-m.stop:
		case <-timeout:
		}
	}()
	return ch
}

func (c *context) HandleDomainMigration(job *que.Job) (err error) {
	log := c.logger.New("fn", "HandleDomainMigration")
	log.Info("handling domain migration", "job_id", job.ID, "error_count", job.ErrorCount)

	var dm *ct.DomainMigration
	if err := json.Unmarshal(job.Args, &dm); err != nil {
		log.Error("error unmarshaling job", "err", err)
		return err
	}

	log = log.New("domain_migration", dm.ID)

	m := &migration{
		db:                 c.db,
		client:             c.client,
		rc:                 routerc.New(),
		logger:             log,
		dm:                 dm,
		activeRouteUpdates: make(chan struct{}, maxActiveRouteUpdates),
		stop:               job.Stop,
	}

	if err := m.db.QueryRow("SELECT old_domain, domain, old_tls_cert, tls_cert, created_at, finished_at FROM domain_migrations WHERE migration_id = $1", dm.ID).Scan(&dm.OldDomain, &dm.Domain, &dm.OldTLSCert, &dm.TLSCert, &dm.CreatedAt, &dm.FinishedAt); err != nil {
		log.Error("error fetching postgres record", "err", err)
		m.createEvent(err)
		return err
	}

	if dm.FinishedAt != nil {
		// Already done
		return nil
	}

	return m.Run()
}

func (m *migration) Run() error {
	log := m.logger
	dm := m.dm

	// Generate TLS Cert if not already present
	var cert *tlscert.Cert
	if dm.TLSCert == nil {
		var err error
		cert, err = m.generateTLSCert()
		if err != nil {
			log.Error("error generating TLS cert", "err", err)
			m.createEvent(err)
			return err
		}
		dm.TLSCert = cert
	}

	if err := m.maybeDeployController(); err != nil {
		log.Error("error deploying controller", "err", err)
		m.createEvent(err)
		return err
	}

	if err := m.maybeDeployRouter(); err != nil {
		log.Error("error deploying router", "err", err)
		m.createEvent(err)
		return err
	}

	if err := m.maybeDeployDashboard(); err != nil {
		log.Error("error deploying dashboard", "err", err)
		m.createEvent(err)
		return err
	}

	if err := m.createMissingRoutes(); err != nil {
		log.Error("error creating missing routes", "err", err)
		m.createEvent(err)
		return err
	}

	if err := m.db.QueryRow("UPDATE domain_migrations SET finished_at = now() RETURNING finished_at").Scan(&dm.FinishedAt); err != nil {
		log.Error("error setting finished_at", "err", err)
		m.createEvent(err)
		return err
	}

	log.Info("domain migration complete")

	m.createEvent(nil)

	return nil
}

func (m *migration) generateTLSCert() (*tlscert.Cert, error) {
	hosts := []string{
		m.dm.Domain,
		fmt.Sprintf("*.%s", m.dm.Domain),
	}
	cert, err := tlscert.Generate(hosts)
	if err != nil {
		return nil, err
	}
	if _, err := m.db.Query("UPDATE domain_migrations SET tls_cert = $1 WHERE migration_id = $2", cert, m.dm.ID); err != nil {
		return nil, err
	}
	return cert, nil
}

func dupRelease(release *ct.Release) *ct.Release {
	return &ct.Release{
		AppID:       release.AppID,
		ArtifactIDs: release.ArtifactIDs,
		Env:         release.Env,
		Meta:        release.Meta,
		Processes:   release.Processes,
	}
}

func (m *migration) waitForDeployment(app string) error {
	list, err := m.client.DeploymentList(app)
	if err != nil || len(list) == 0 {
		return err
	}
	d := list[0]
	if d.Status != "pending" && d.Status != "running" {
		return nil
	}

	events := make(chan *ct.Event)
	stream, err := m.client.StreamEvents(ct.StreamEventsOptions{
		AppID:       d.AppID,
		ObjectID:    d.ID,
		ObjectTypes: []ct.EventType{ct.EventTypeDeployment},
	}, events)
	if err != nil {
		return err
	}
	defer stream.Close()

	log := m.logger.New("app.name", app, "deployment", d.ID)
	log.Info("waiting for deployment")

	timeout := time.After(2 * time.Minute)
	for {
		select {
		case event := <-events:
			var data *ct.DeploymentEvent
			if err := json.Unmarshal(event.Data, &data); err != nil {
				return err
			}
			if data.Status == "complete" {
				log.Info("deployment complete")
				return nil
			}
			if data.Status == "failed" {
				log.Error("deployment failed", "error", data.Error)
				return errors.New(data.Error)
			}
		case <-timeout:
			err := errors.New("timed out waiting for deployment")
			log.Error(err.Error())
			return err
		case <-m.stop:
			return worker.ErrStopped
		}
	}
}

func (m *migration) maybeDeployController() error {
	const appName = "controller"
	log := m.logger.New("app.name", appName)
	if err := m.waitForDeployment(appName); err != nil {
		return err
	}
	release, err := m.client.GetAppRelease(appName)
	if err != nil {
		log.Error("error fetching release", "error", err)
		return err
	}
	if release.Env["DEFAULT_ROUTE_DOMAIN"] != m.dm.OldDomain {
		log.Info("already migrated")
		return nil
	}
	release = dupRelease(release)
	release.Env["DEFAULT_ROUTE_DOMAIN"] = m.dm.Domain
	release.Env["CA_CERT"] = m.dm.TLSCert.CACert
	if err := m.client.CreateRelease(release.AppID, release); err != nil {
		log.Error("error creating release", "error", err)
		return err
	}
	if err := m.client.DeployAppRelease(release.AppID, release.ID, m.cancelDeploy()); err != nil {
		log.Error("error deploying release", "error", err)
		select {
		case <-m.stop:
			return worker.ErrStopped
		default:
			return err
		}
	}
	return nil
}

func (m *migration) maybeDeployRouter() error {
	const appName = "router"
	log := m.logger.New("app.name", appName)
	if err := m.waitForDeployment(appName); err != nil {
		return err
	}
	release, err := m.client.GetAppRelease(appName)
	if err != nil {
		log.Error("error fetching release", "error", err)
		return err
	}
	if release.Env["TLSCERT"] != m.dm.OldTLSCert.Cert {
		log.Info("already migrated")
		return nil
	}
	release = dupRelease(release)
	release.Env["TLSCERT"] = m.dm.TLSCert.Cert
	release.Env["TLSKEY"] = m.dm.TLSCert.PrivateKey
	if err := m.client.CreateRelease(release.AppID, release); err != nil {
		log.Error("error creating release", "error", err)
		return err
	}
	if err := m.client.DeployAppRelease(release.AppID, release.ID, m.cancelDeploy()); err != nil {
		log.Error("error deploying release", "error", err)
		select {
		case <-m.stop:
			return worker.ErrStopped
		default:
			return err
		}
	}
	return nil
}

func (m *migration) maybeDeployDashboard() error {
	const appName = "dashboard"
	log := m.logger.New("app.name", appName)
	if err := m.waitForDeployment(appName); err != nil {
		return err
	}
	release, err := m.client.GetAppRelease(appName)
	if err != nil {
		log.Error("error fetching release", "error", err)
		return err
	}
	if release.Env["DEFAULT_ROUTE_DOMAIN"] != m.dm.OldDomain {
		log.Info("already migrated")
		return nil
	}
	release = dupRelease(release)
	release.Env["CA_CERT"] = m.dm.TLSCert.CACert
	release.Env["DEFAULT_ROUTE_DOMAIN"] = m.dm.Domain
	release.Env["CONTROLLER_DOMAIN"] = fmt.Sprintf("controller.%s", m.dm.Domain)
	release.Env["URL"] = fmt.Sprintf("https://dashboard.%s", m.dm.Domain)
	if err := m.client.CreateRelease(release.AppID, release); err != nil {
		log.Error("error creating release", "error", err)
		return err
	}
	if err := m.client.DeployAppRelease(release.AppID, release.ID, m.cancelDeploy()); err != nil {
		log.Error("error deploying release", "error", err)
		select {
		case <-m.stop:
			return worker.ErrStopped
		default:
			return err
		}
	}
	return nil
}

func (m *migration) createMissingRoutes() error {
	// Get list of all routes from router
	routes, err := m.rc.ListRoutes("")
	if err != nil {
		return err
	}

	apps, err := m.client.AppList()
	if err != nil {
		return err
	}

	// Index routes by appID
	appRoutes := make(map[string][]*router.Route, len(apps))
	for _, r := range routes {
		if !strings.HasPrefix(r.ParentRef, ct.RouteParentRefPrefix) {
			continue
		}
		appID := strings.TrimPrefix(r.ParentRef, ct.RouteParentRefPrefix)
		if appRoutes[appID] == nil {
			appRoutes[appID] = make([]*router.Route, 0, 1)
		}
		appRoutes[appID] = append(appRoutes[appID], r)
	}

	errChan := make(chan error, len(apps))
	createMissingRoutes := func(app *ct.App) {
		errChan <- m.appCreateMissingRoutes(app.ID, appRoutes[app.ID])
	}
	for _, app := range apps {
		go createMissingRoutes(app)
	}
	var returnErr error
	for range apps {
		if err := <-errChan; err != nil && returnErr == nil {
			returnErr = err
		}
	}
	return returnErr
}

func (m *migration) appCreateMissingRoutes(appID string, routes []*router.Route) error {
	if routes == nil {
		// There are no routes for this app
		return nil
	}
	m.activeRouteUpdates <- struct{}{}
	for _, route := range routes {
		if route.Type != "http" {
			continue
		}
		if strings.HasSuffix(route.Domain, m.dm.OldDomain) {
			if err := m.appMaybeCreateRoute(appID, route, routes); err != nil {
				return err
			}
		}
	}
	<-m.activeRouteUpdates
	return nil
}

func (m *migration) appMaybeCreateRoute(appID string, oldRoute *router.Route, routes []*router.Route) error {
	prefix := strings.TrimSuffix(oldRoute.Domain, m.dm.OldDomain)
	for _, route := range routes {
		if strings.HasPrefix(route.Domain, prefix) && strings.HasSuffix(route.Domain, m.dm.Domain) {
			// Route already exists
			return nil
		}
	}
	route := &router.Route{
		Type:          "http",
		Domain:        strings.Join([]string{prefix, m.dm.Domain}, ""),
		Sticky:        oldRoute.Sticky,
		Service:       oldRoute.Service,
		DrainBackends: oldRoute.DrainBackends,
	}
	if oldRoute.Certificate != nil && oldRoute.Certificate.Cert == strings.TrimSpace(m.dm.OldTLSCert.Cert) {
		route.Certificate = &router.Certificate{
			Cert: m.dm.TLSCert.Cert,
			Key:  m.dm.TLSCert.PrivateKey,
		}
	} else {
		route.Certificate = oldRoute.Certificate
	}
	err := m.client.CreateRoute(appID, route)
	if err != nil && err.Error() == "conflict: Duplicate route" {
		return nil
	}
	return err
}

func (m *migration) createEvent(err error) error {
	if err == worker.ErrStopped {
		return nil
	}
	e := ct.DomainMigrationEvent{DomainMigration: m.dm}
	if err != nil {
		e.Error = err.Error()
	}
	query := "INSERT INTO events (object_id, object_type, data) VALUES ($1, $2, $3)"
	return m.db.Exec(query, m.dm.ID, string(ct.EventTypeDomainMigration), e)
}
