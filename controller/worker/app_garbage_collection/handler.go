package app_garbage_collection

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/que-go"
	"github.com/inconshreveable/log15"
)

type context struct {
	db     *postgres.DB
	client controller.Client
	logger log15.Logger
}

func JobHandler(db *postgres.DB, client controller.Client, logger log15.Logger) func(*que.Job) error {
	return (&context{db, client, logger}).HandleAppGarbageCollection
}

func (c *context) HandleAppGarbageCollection(job *que.Job) (err error) {
	log := c.logger.New("fn", "HandleAppGarbageCollection")
	log.Info("handling garbage collection", "job_id", job.ID, "error_count", job.ErrorCount)

	var gc ct.AppGarbageCollection
	if err := json.Unmarshal(job.Args, &gc); err != nil {
		log.Error("error unmarshaling job", "err", err)
		return err
	}

	log = log.New("app.id", gc.AppID)
	defer func() {
		if err := c.createEvent(&gc, err); err != nil {
			log.Error("error creating garbage collection event", "err", err)
		}
		log.Info("garbage collection finished")
	}()

	log.Info("getting app")
	app, err := c.client.GetApp(gc.AppID)
	if err != nil {
		log.Error("error getting app", "err", err)
		return err
	}

	log.Info("deleting old slug releases")
	meta, ok := app.Meta["gc.max_inactive_slug_releases"]
	if !ok || meta == "false" {
		log.Info(fmt.Sprintf("skipping old slug release deletion since gc.max_inactive_slug_releases=%q", meta))
		return nil
	}
	maxInactiveSlugReleases, err := strconv.Atoi(meta)
	if err != nil {
		log.Error("error parsing gc.max_inactive_slug_releases", "err", err)
		return err
	}
	log.Info(fmt.Sprintf("gc.max_inactive_slug_releases is set to %d", maxInactiveSlugReleases))

	log.Info("getting app releases")
	releases, err := c.client.AppReleaseList(app.ID)
	if err != nil {
		log.Error("error getting app releases", "err", err)
		return err
	}
	log.Info("getting app formations")
	formations, err := c.client.FormationList(app.ID)
	if err != nil {
		log.Error("error getting app formations", "err", err)
		return err
	}

	// determine which releases are active so we don't delete them
	activeReleases := make(map[string]struct{}, len(formations))
outer:
	for _, formation := range formations {
		for _, n := range formation.Processes {
			if n > 0 {
				activeReleases[formation.ReleaseID] = struct{}{}
				continue outer
			}
		}
	}

	// iterate over the releases (which are in reverse chronological order)
	// and mark them for deletion once we have seen more than the
	// configured maximum count of slugs with distinct URIs
	oldReleases := make([]*ct.Release, 0, len(releases))
	distinctSlugs := make(map[string]struct{}, len(releases))
	for _, release := range releases {
		// ignore active or non-slug releases
		if _, ok := activeReleases[release.ID]; ok || !release.IsGitDeploy() {
			continue
		}

		if len(distinctSlugs) >= maxInactiveSlugReleases {
			oldReleases = append(oldReleases, release)
		}

		if len(release.ArtifactIDs) < 2 {
			continue
		}
		id := release.ArtifactIDs[1]
		artifact, err := c.client.GetArtifact(id)
		if err != nil {
			log.Error("error getting slug artifact for release", "release.id", release.ID, "artifact.id", id, "err", err)
			return err
		}
		if artifact.Blobstore() {
			distinctSlugs[artifact.URI] = struct{}{}
		}
	}
	log.Info(fmt.Sprintf("app has %d releases (%d with distinct slugs)", len(releases), len(distinctSlugs)))

	if len(oldReleases) == 0 {
		log.Info("no old releases to delete")
		return nil
	}

	log.Info(fmt.Sprintf("deleting %d old releases", len(oldReleases)))
	gc.DeletedReleases = make([]string, 0, len(oldReleases))
	for _, release := range oldReleases {
		log.Info("deleting release", "release.id", release.ID)
		if _, err := c.client.DeleteRelease(app.ID, release.ID); err != nil {
			// ignore releases which fail to delete, the next gc cycle
			// will try again
			log.Error("error deleting release", "release.id", release.ID, "err", err)
			continue
		}
		gc.DeletedReleases = append(gc.DeletedReleases, release.ID)
	}

	return nil
}

func (c *context) createEvent(gc *ct.AppGarbageCollection, err error) error {
	e := ct.AppGarbageCollectionEvent{AppGarbageCollection: gc}
	if err != nil {
		e.Error = err.Error()
	}
	return c.db.Exec("event_insert", gc.AppID, gc.AppID, string(ct.EventTypeAppGarbageCollection), e)
}
