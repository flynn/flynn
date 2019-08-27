package domain_migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	worker "github.com/flynn/flynn/controller/worker/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/tlscert"
	router "github.com/flynn/flynn/router/types"
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
	logger             log15.Logger
	dm                 *ct.DomainMigration
	activeRouteUpdates chan struct{}
	stop               chan struct{}
}

func (m *migration) MigrateApp(appName string, newEnv map[string]string) error {
	log := m.logger.New("app.name", appName)
	if err := m.waitForDeployment(appName); err != nil {
		return err
	}
	release, err := m.client.GetAppRelease(appName)
	if err != nil {
		log.Error("error fetching release", "error", err)
		return err
	}
	newRelease := dupRelease(release)
	for k, v := range newEnv {
		if release.Env[k] == v {
			log.Info("already migrated")
			return nil
		}
		newRelease.Env[k] = v
	}
	if err := m.client.CreateRelease(newRelease.AppID, newRelease); err != nil {
		log.Error("error creating release", "error", err)
		return err
	}
	if err := m.client.DeployAppRelease(newRelease.AppID, newRelease.ID, m.cancelDeploy()); err != nil {
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

	type AppMigration struct {
		AppName string
		NewEnv  map[string]string
	}

	appMigrations := []*AppMigration{
		{
			AppName: "controller",
			NewEnv: map[string]string{
				"DEFAULT_ROUTE_DOMAIN": m.dm.Domain,
				"CA_CERT":              m.dm.TLSCert.CACert,
			},
		},
		{
			AppName: "router",
			NewEnv: map[string]string{
				"TLSCERT": m.dm.TLSCert.Cert,
				"TLSKEY":  m.dm.TLSCert.PrivateKey,
			},
		},
		{
			AppName: "dashboard",
			NewEnv: map[string]string{
				"CA_CERT":              m.dm.TLSCert.CACert,
				"DEFAULT_ROUTE_DOMAIN": m.dm.Domain,
				"CONTROLLER_DOMAIN":    fmt.Sprintf("controller.%s", m.dm.Domain),
				"URL":                  fmt.Sprintf("https://dashboard.%s", m.dm.Domain),
			},
		},
		{
			AppName: "dashboardv2",
			NewEnv: map[string]string{
				"DEFAULT_ROUTE_DOMAIN": m.dm.Domain,
				"CONTROLLER_DOMAIN":    fmt.Sprintf("controller-grpc.%s", m.dm.Domain),
				"CONTROLLER_HOST":      fmt.Sprintf("https://controller-grpc.%s", m.dm.Domain),
				"INTERFACE_URL":        fmt.Sprintf("https://dashboardv2.%s", m.dm.Domain),
			},
		},
	}

	for _, am := range appMigrations {
		if err := m.MigrateApp(am.AppName, am.NewEnv); err != nil {
			log.Error(fmt.Sprintf("error deploying %s", am.AppName), "err", err)
			m.createEvent(err)
			return err
		}
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

func (m *migration) createMissingRoutes() error {
	apps, err := m.client.AppList()
	if err != nil {
		return err
	}

	errChan := make(chan error, len(apps))
	createMissingRoutes := func(app *ct.App) {
		errChan <- m.appCreateMissingRoutes(app.ID)
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

func (m *migration) appCreateMissingRoutes(appID string) error {
	routes, err := m.client.AppRouteList(appID)
	if err != nil {
		return err
	}
	if len(routes) == 0 {
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
