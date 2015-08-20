package app_deletion

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/que-go"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
)

type context struct {
	db     *postgres.DB
	client *controller.Client
	logger log15.Logger
}

func JobHandler(db *postgres.DB, client *controller.Client, logger log15.Logger) func(*que.Job) error {
	return (&context{db, client, logger}).HandleAppDeletion
}

func (c *context) HandleAppDeletion(job *que.Job) (err error) {
	log := c.logger.New("fn", "HandleAppDeletion")
	log.Info("handling app deletion", "job_id", job.ID, "error_count", job.ErrorCount)

	var app ct.App
	if err := json.Unmarshal(job.Args, &app); err != nil {
		log.Error("error unmarshaling job", "err", err)
		return err
	}
	log = log.New("app_id", app.ID)

	a := ct.AppDeletion{AppID: app.ID}
	defer func() { c.createEvent(&a, err) }()

	log.Info("getting app routes")
	routes, err := c.client.RouteList(app.ID)
	if err != nil {
		log.Error("error getting app routes", "err", err)
		return err
	}
	for _, route := range routes {
		log.Info("deleting route", "route_id", route.FormattedID())
		if err := c.client.DeleteRoute(app.ID, route.FormattedID()); err != nil {
			log.Info("error deleting route", "route_id", route.FormattedID(), "err", err)
			return err
		}
		a.DeletedRoutes = append(a.DeletedRoutes, route)
	}
	log.Info(fmt.Sprintf("deleted %d routes", len(a.DeletedRoutes)))

	log.Info("getting app resources")
	resources, err := c.client.AppResourceList(app.ID)
	if err != nil {
		log.Error("error getting app resources", "err", err)
		return err
	}
	for _, resource := range resources {
		// don't delete resources still in use by other apps.
		if len(resource.Apps) > 1 {
			continue
		}
		log.Info("deleting resource", "provider_id", resource.ProviderID, "resource_id", resource.ID)
		if err := c.client.DeleteResource(resource.ProviderID, resource.ID); err != nil {
			log.Error("error deleting resource", "provider_id", resource.ProviderID, "resource_id", resource.ID, "err", err)
			return err
		}
		a.DeletedResources = append(a.DeletedResources, resource)
	}
	log.Info(fmt.Sprintf("deleted %d resources", len(a.DeletedResources)))

	log.Info("cleaning app cache")
	// TODO: share URL construction with gitreceive / flynn-receive
	for _, cacheURL := range []string{
		fmt.Sprintf("http://blobstore.discoverd/repos/%s.tar", app.ID),
		fmt.Sprintf("http://blobstore.discoverd/%s-cache.tgz", app.ID),
	} {
		req, err := http.NewRequest("DELETE", cacheURL, nil)
		if err != nil {
			log.Error("error creating app cache delete request", "err", err)
			return err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Error("error performing app cache delete request", "err", err)
			return err
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNotFound {
			e := fmt.Sprintf("unexpected status code %d cleaning cache URL %s", res.StatusCode, cacheURL)
			log.Error(e)
			return errors.New(e)
		}
	}

	log.Info("deleting app")
	tx, err := c.db.Begin()
	if err != nil {
		log.Error("error starting db transaction", "err", err)
		return err
	}
	_, err = tx.Exec("UPDATE apps SET deleted_at = now() WHERE app_id = $1 AND deleted_at IS NULL", app.ID)
	if err != nil {
		log.Error("error executing app deletion query", "err", err)
		tx.Rollback()
		return err
	}
	_, err = tx.Exec("UPDATE formations SET deleted_at = now(), processes = NULL, updated_at = now() WHERE app_id = $1 AND deleted_at IS NULL", app.ID)
	if err != nil {
		log.Error("error executing formation deletion query", "err", err)
		tx.Rollback()
		return err
	}
	_, err = tx.Exec("UPDATE app_resources SET deleted_at = now() WHERE app_id = $1 AND deleted_at IS NULL", app.ID)
	if err != nil {
		log.Error("error executing resource deletion query", "err", err)
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (c *context) createEvent(a *ct.AppDeletion, err error) error {
	e := ct.AppDeletionEvent{AppDeletion: a}
	if err != nil {
		e.Error = err.Error()
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	query := "INSERT INTO events (app_id, object_id, object_type, data) VALUES ($1, $2, $3, $4)"
	return c.db.Exec(query, a.AppID, a.AppID, string(ct.EventTypeAppDeletion), data)
}
