package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/jackc/pgx"
	"golang.org/x/net/context"
)

/* Job Stuff */
type JobRepo struct {
	db *postgres.DB
}

func NewJobRepo(db *postgres.DB) *JobRepo {
	return &JobRepo{db}
}

func (r *JobRepo) Get(id string) (*ct.Job, error) {
	if !idPattern.MatchString(id) {
		var err error
		id, err = cluster.ExtractUUID(id)
		if err != nil {
			return nil, ErrNotFound
		}
	}
	row := r.db.QueryRow("job_select", id)
	return scanJob(row)
}

func (r *JobRepo) Add(job *ct.Job) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	// TODO: actually validate
	err = tx.QueryRow(
		"job_insert",
		job.ID,
		job.UUID,
		job.HostID,
		job.AppID,
		job.ReleaseID,
		job.Type,
		string(job.State),
		job.Meta,
		job.ExitStatus,
		job.HostError,
		job.RunAt,
		job.Restarts,
		job.Args,
	).Scan(&job.CreatedAt, &job.UpdatedAt)
	if postgres.IsPostgresCode(err, postgres.CheckViolation) {
		tx.Rollback()
		return ct.ValidationError{Field: "state", Message: err.Error()}
	}
	if err != nil {
		tx.Rollback()
		return err
	}

	for i, volID := range job.VolumeIDs {
		if err := tx.Exec("job_volume_insert", job.UUID, volID, i); err != nil {
			tx.Rollback()
			return err
		}
	}

	// create a job event, ignoring possible duplications
	uniqueID := strings.Join([]string{job.UUID, string(job.State)}, "|")
	if err := tx.Exec("event_insert_unique", job.AppID, job.UUID, uniqueID, string(ct.EventTypeJob), job); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func scanJob(s postgres.Scanner) (*ct.Job, error) {
	job := &ct.Job{}
	var state string
	var volumeIDs string
	err := s.Scan(
		&job.ID,
		&job.UUID,
		&job.HostID,
		&job.AppID,
		&job.ReleaseID,
		&job.Type,
		&state,
		&job.Meta,
		&job.ExitStatus,
		&job.HostError,
		&job.RunAt,
		&job.Restarts,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.Args,
		&volumeIDs,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	job.State = ct.JobState(state)
	if volumeIDs != "" {
		job.VolumeIDs = split(volumeIDs[1:len(volumeIDs)-1], ",")
	}
	return job, nil
}

func (r *JobRepo) List(appID string) ([]*ct.Job, error) {
	rows, err := r.db.Query("job_list", appID)
	if err != nil {
		return nil, err
	}
	jobs := []*ct.Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (r *JobRepo) ListActive() ([]*ct.Job, error) {
	rows, err := r.db.Query("job_list_active")
	if err != nil {
		return nil, err
	}
	jobs := []*ct.Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (c *controllerAPI) ListJobs(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	app := c.getApp(ctx)
	list, err := c.jobRepo.List(app.ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, list)
}

func (c *controllerAPI) ListActiveJobs(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	list, err := c.jobRepo.ListActive()
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, list)
}

func (c *controllerAPI) GetJob(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	job, err := c.jobRepo.Get(params.ByName("jobs_id"))
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, job)
}

func (c *controllerAPI) PutJob(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	app := c.getApp(ctx)

	var job ct.Job
	if err := httphelper.DecodeJSON(req, &job); err != nil {
		respondWithError(w, err)
		return
	}

	job.AppID = app.ID

	if err := schema.Validate(job); err != nil {
		respondWithError(w, err)
		return
	}

	if err := c.jobRepo.Add(&job); err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &job)
}

func (c *controllerAPI) KillJob(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	job, err := c.jobRepo.Get(params.ByName("jobs_id"))
	if err != nil {
		respondWithError(w, err)
		return
	} else if job.HostID == "" {
		httphelper.ValidationError(w, "", "cannot kill a job which has not been placed on a host")
		return
	}

	client, err := c.clusterClient.Host(job.HostID)
	if err != nil {
		respondWithError(w, err)
		return
	}

	if err = client.StopJob(job.ID); err != nil {
		if _, ok := err.(ct.NotFoundError); ok {
			err = ErrNotFound
		}
		respondWithError(w, err)
		return
	}
}

var runJobAttempts = attempt.Strategy{
	Total: 30 * time.Second,
	Delay: 100 * time.Millisecond,
}

func (c *controllerAPI) RunJob(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var newJob ct.NewJob
	if err := httphelper.DecodeJSON(req, &newJob); err != nil {
		respondWithError(w, err)
		return
	}

	if err := schema.Validate(newJob); err != nil {
		respondWithError(w, err)
		return
	}

	data, err := c.releaseRepo.Get(newJob.ReleaseID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	release := data.(*ct.Release)
	var artifactIDs []string
	if len(newJob.ArtifactIDs) > 0 {
		artifactIDs = newJob.ArtifactIDs
	} else if len(release.ArtifactIDs) > 0 {
		artifactIDs = release.ArtifactIDs
	} else {
		httphelper.ValidationError(w, "release.ArtifactIDs", "cannot be empty")
		return
	}

	artifacts := make([]*ct.Artifact, len(artifactIDs))
	artifactList, err := c.artifactRepo.ListIDs(artifactIDs...)
	if err != nil {
		respondWithError(w, err)
		return
	}
	for i, id := range artifactIDs {
		artifacts[i] = artifactList[id]
	}

	var entrypoint ct.ImageEntrypoint
	if e := utils.GetEntrypoint(artifacts, ""); e != nil {
		entrypoint = *e
	}

	attach := strings.Contains(req.Header.Get("Upgrade"), "flynn-attach/0")

	hosts, err := c.clusterClient.Hosts()
	if err != nil {
		respondWithError(w, err)
		return
	}
	if len(hosts) == 0 {
		respondWithError(w, errors.New("no hosts found"))
		return
	}
	client := hosts[random.Math.Intn(len(hosts))]

	uuid := random.UUID()
	hostID := client.ID()
	id := cluster.GenerateJobID(hostID, uuid)
	app := c.getApp(ctx)
	env := make(map[string]string, len(entrypoint.Env)+len(release.Env)+len(newJob.Env)+4)
	env["FLYNN_APP_ID"] = app.ID
	env["FLYNN_RELEASE_ID"] = release.ID
	env["FLYNN_PROCESS_TYPE"] = ""
	env["FLYNN_JOB_ID"] = id
	for k, v := range entrypoint.Env {
		env[k] = v
	}
	if newJob.ReleaseEnv {
		for k, v := range release.Env {
			env[k] = v
		}
	}
	for k, v := range newJob.Env {
		env[k] = v
	}
	metadata := make(map[string]string, len(newJob.Meta)+3)
	for k, v := range newJob.Meta {
		metadata[k] = v
	}
	metadata["flynn-controller.app"] = app.ID
	metadata["flynn-controller.app_name"] = app.Name
	metadata["flynn-controller.release"] = release.ID
	job := &host.Job{
		ID:       id,
		Metadata: metadata,
		Config: host.ContainerConfig{
			Args:       entrypoint.Args,
			Env:        env,
			WorkingDir: entrypoint.WorkingDir,
			Uid:        entrypoint.Uid,
			Gid:        entrypoint.Gid,
			TTY:        newJob.TTY,
			Stdin:      attach,
			DisableLog: newJob.DisableLog,
		},
		Resources: newJob.Resources,
		Partition: string(newJob.Partition),
	}
	resource.SetDefaults(&job.Resources)
	if len(newJob.Args) > 0 {
		job.Config.Args = newJob.Args
	}
	utils.SetupMountspecs(job, artifacts)

	// provision data volume if required
	if newJob.Data {
		vol := &ct.VolumeReq{Path: "/data", DeleteOnStop: true}
		if _, err := utils.ProvisionVolume(vol, client, job); err != nil {
			respondWithError(w, err)
			return
		}
	}

	var attachClient cluster.AttachClient
	if attach {
		attachReq := &host.AttachReq{
			JobID:  job.ID,
			Flags:  host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagStdin | host.AttachFlagStream,
			Height: uint16(newJob.Lines),
			Width:  uint16(newJob.Columns),
		}
		attachClient, err = client.Attach(attachReq, true)
		if err != nil {
			respondWithError(w, fmt.Errorf("attach failed: %s", err.Error()))
			return
		}
		defer attachClient.Close()
	}

	err = runJobAttempts.RunWithValidator(func() error {
		return client.AddJob(job)
	}, httphelper.IsRetryableError)
	if err != nil {
		respondWithError(w, fmt.Errorf("schedule failed: %s", err.Error()))
		return
	}

	if attach {
		// TODO(titanous): This Wait could block indefinitely if something goes
		// wrong, a context should be threaded in that cancels if the client
		// goes away.
		if err := attachClient.Wait(); err != nil {
			respondWithError(w, fmt.Errorf("attach wait failed: %s", err.Error()))
			return
		}
		w.Header().Set("Connection", "upgrade")
		w.Header().Set("Upgrade", "flynn-attach/0")
		w.WriteHeader(http.StatusSwitchingProtocols)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			panic(err)
		}
		defer conn.Close()

		done := make(chan struct{}, 2)
		cp := func(to io.Writer, from io.Reader) {
			io.Copy(to, from)
			done <- struct{}{}
		}
		go cp(conn, attachClient.Conn())
		go cp(attachClient.Conn(), conn)

		// Wait for one of the connections to be closed or interrupted. EOF is
		// framed inside the attach protocol, so a read/write error indicates
		// that we're done and should clean up.
		<-done

		return
	} else {
		httphelper.JSON(w, 200, &ct.Job{
			ID:        job.ID,
			UUID:      uuid,
			HostID:    hostID,
			ReleaseID: newJob.ReleaseID,
			Args:      newJob.Args,
		})
	}
}
