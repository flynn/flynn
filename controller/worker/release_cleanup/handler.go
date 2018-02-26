package release_cleanup

import (
	"encoding/json"
	"fmt"
	"net/http"

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
	return (&context{db, client, logger}).HandleReleaseCleanup
}

func (c *context) HandleReleaseCleanup(job *que.Job) (err error) {
	log := c.logger.New("fn", "HandleReleaseCleanup")
	log.Info("handling release cleanup", "job_id", job.ID, "error_count", job.ErrorCount)

	var data struct {
		AppID     string
		ReleaseID string
		FileURIs  []string
	}
	if err := json.Unmarshal(job.Args, &data); err != nil {
		log.Error("error unmarshaling job", "err", err)
		return err
	}
	log = log.New("release_id", data.ReleaseID)

	r := ct.ReleaseDeletion{AppID: data.AppID, ReleaseID: data.ReleaseID}
	defer func() { c.createEvent(&r, err) }()

	for _, uri := range data.FileURIs {
		log.Info("deleting file", "uri", uri)
		if err := deleteFile(uri); err != nil {
			log.Error("error deleting file", "err", err)
			return err
		}
		r.DeletedFiles = append(r.DeletedFiles, uri)
	}
	log.Info(fmt.Sprintf("deleted %d files", len(r.DeletedFiles)))

	return nil
}

func deleteFile(uri string) error {
	req, err := http.NewRequest("DELETE", uri, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted && res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("unexpected status %d", res.StatusCode)
	}
	return nil
}

func (c *context) createEvent(r *ct.ReleaseDeletion, err error) error {
	e := ct.ReleaseDeletionEvent{ReleaseDeletion: r}
	if err != nil {
		e.Error = err.Error()
	}
	return c.db.Exec("event_insert", r.AppID, r.ReleaseID, string(ct.EventTypeReleaseDeletion), e)
}
