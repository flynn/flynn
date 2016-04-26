package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
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
	db        *postgres.DB
	artifacts *ArtifactRepo
}

func NewJobRepo(db *postgres.DB, artifacts *ArtifactRepo) *JobRepo {
	return &JobRepo{db, artifacts}
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
	// TODO: actually validate
	err := r.db.QueryRow(
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
	).Scan(&job.CreatedAt, &job.UpdatedAt)
	if postgres.IsUniquenessError(err, "") {
		err = r.db.QueryRow(
			"job_update",
			job.UUID,
			job.ID,
			job.HostID,
			string(job.State),
			job.ExitStatus,
			job.HostError,
			job.RunAt,
			job.Restarts,
		).Scan(&job.CreatedAt, &job.UpdatedAt)
		if postgres.IsPostgresCode(err, postgres.CheckViolation) {
			return ct.ValidationError{Field: "state", Message: err.Error()}
		}
	}
	if err != nil {
		return err
	}

	// create a job event, ignoring possible duplications
	uniqueID := strings.Join([]string{job.UUID, string(job.State)}, "|")
	err = r.db.Exec("event_insert_unique", job.AppID, job.UUID, uniqueID, string(ct.EventTypeJob), job)
	if postgres.IsUniquenessError(err, "") {
		return nil
	} else if err != nil {
		return err
	}

	if job.JobRequestID == "" {
		return nil
	}

	// update the job request
	req, err := r.GetJobRequest(job.JobRequestID)
	if err != nil {
		return err
	}
	req.JobID = job.UUID
	req.Error = job.HostError

	switch job.State {
	case ct.JobStatePending:
		req.State = ct.JobRequestStatePending
	case ct.JobStateStarting:
		req.State = ct.JobRequestStateStarting
	case ct.JobStateUp, ct.JobStateStopping:
		req.State = ct.JobRequestStateRunning
	case ct.JobStateDown:
		if job.HostError != nil {
			req.State = ct.JobRequestStateFailed
		} else {
			req.State = ct.JobRequestStateSucceeded
		}
	}
	if err := r.db.Exec("job_request_update", req.ID, req.JobID, string(req.State), req.Error); err != nil {
		return err
	}

	// create a job request event, ignoring possible duplications
	uniqueID = strings.Join([]string{req.ID, req.JobID, string(req.State)}, "|")
	err = r.db.Exec("event_insert_unique", job.AppID, req.ID, uniqueID, string(ct.EventTypeJobRequest), req)
	if postgres.IsUniquenessError(err, "") {
		return nil
	}
	return err
}

func (r *JobRepo) AddJobRequest(req *ct.JobRequest) error {
	if req.ID == "" {
		req.ID = random.UUID()
	}
	if req.Config == nil {
		req.Config = &ct.JobConfig{}
	}
	if req.Config.Type == "" {
		req.Config.Type = "run"
	}
	req.State = ct.JobRequestStatePending
	resource.SetDefaults(&req.Config.Resources)

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	err = tx.QueryRow(
		"job_request_insert",
		req.ID,
		req.AppID,
		req.ReleaseID,
		string(req.State),
		req.Config,
	).Scan(&req.CreatedAt)
	if err != nil {
		tx.Rollback()
		return err
	}

	for _, artifactID := range req.ArtifactIDs {
		if err := tx.Exec("job_request_artifacts_insert", req.ID, artifactID); err != nil {
			tx.Rollback()
			return err
		}
	}

	if err := createEvent(tx.Exec, &ct.Event{
		AppID:      req.AppID,
		ObjectID:   req.ID,
		ObjectType: ct.EventTypeJobRequest,
	}, req); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (r *JobRepo) GetJobRequest(id string) (*ct.JobRequest, error) {
	var req ct.JobRequest
	var jobID *string
	var artifactIDs string
	var state string
	err := r.db.QueryRow("job_request_select", id).Scan(
		&req.ID,
		&jobID,
		&req.AppID,
		&req.ReleaseID,
		&artifactIDs,
		&state,
		&req.Config,
		&req.Error,
		&req.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if jobID != nil {
		req.JobID = *jobID
	}
	if artifactIDs != "" {
		req.ArtifactIDs = split(artifactIDs[1:len(artifactIDs)-1], ",")
	}
	req.State = ct.JobRequestState(state)
	return &req, nil
}

func scanJob(s postgres.Scanner) (*ct.Job, error) {
	job := &ct.Job{}
	var state string
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
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	job.State = ct.JobState(state)
	return job, nil
}

func scanExpandedJobRequest(s postgres.Scanner) (*ct.ExpandedJobRequest, []string, error) {
	req := &ct.ExpandedJobRequest{
		App:     &ct.App{},
		Release: &ct.Release{},
	}
	var jobID *string
	var artifacts string
	var state string
	err := s.Scan(
		&req.ID,
		&jobID,
		&state,
		&req.Config,
		&req.App.ID,
		&req.App.Name,
		&req.App.Meta,
		&req.Release.ID,
		&req.Release.Env,
		&req.Release.Meta,
		&artifacts,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, nil, err
	}
	if jobID != nil {
		req.JobID = *jobID
	}
	var artifactIDs []string
	if artifacts != "" {
		artifactIDs = split(artifacts[1:len(artifacts)-1], ",")
	}
	req.State = ct.JobRequestState(state)
	return req, artifactIDs, nil
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

func (r *JobRepo) ListRequests(state string) ([]*ct.ExpandedJobRequest, error) {
	rows, err := r.db.Query("job_request_list", state)
	if err != nil {
		return nil, err
	}
	var reqs []*ct.ExpandedJobRequest
	for rows.Next() {
		req, artifactIDs, err := scanExpandedJobRequest(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		artifacts, err := r.artifacts.ListIDs(artifactIDs...)
		if err != nil {
			return nil, err
		}
		req.Artifacts = make([]*ct.Artifact, len(artifacts))
		for i, id := range artifactIDs {
			req.Artifacts[i] = artifacts[id]
		}
		reqs = append(reqs, req)
	}
	return reqs, rows.Err()
}

func (c *controllerAPI) connectHost(ctx context.Context) (utils.HostClient, *ct.Job, error) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	job, err := c.jobRepo.Get(params.ByName("jobs_id"))
	if err != nil {
		return nil, nil, err
	} else if job.HostID == "" {
		return nil, nil, errors.New("controller: cannot connect host, job has not been placed in the cluster")
	}
	host, err := c.clusterClient.Host(job.HostID)
	return host, job, err
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

func (c *controllerAPI) ListJobRequests(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	list, err := c.jobRepo.ListRequests(req.URL.Query().Get("state"))
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
	client, job, err := c.connectHost(ctx)
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

func (c *controllerAPI) AttachJob(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	client, job, err := c.connectHost(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}
	var req ct.JobRequest
	if err := httphelper.DecodeJSON(r, &req); err != nil {
		respondWithError(w, err)
		return
	}
	attachReq := &host.AttachReq{
		JobID:  job.ID,
		Flags:  host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagStdin | host.AttachFlagStream,
		Height: uint16(req.Config.Lines),
		Width:  uint16(req.Config.Columns),
	}
	attachClient, err := client.Attach(attachReq, true)
	if err != nil {
		respondWithError(w, fmt.Errorf("attach failed: %s", err.Error()))
		return
	}
	defer attachClient.Close()
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
	<-done
	<-done
}

func (c *controllerAPI) AddJobRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var req ct.JobRequest
	if err := httphelper.DecodeJSON(r, &req); err != nil {
		respondWithError(w, err)
		return
	}
	req.AppID = c.getApp(ctx).ID

	// check the release exists and set the artifact if necessary
	releaseData, err := c.releaseRepo.Get(req.ReleaseID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	release := releaseData.(*ct.Release)
	if len(req.ArtifactIDs) == 0 {
		req.ArtifactIDs = release.ArtifactIDs
	}

	// TODO: validate

	if err := c.jobRepo.AddJobRequest(&req); err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, &req)
}

// RunJob is DEPRECATED, clients should create a job request and wait for it to be scheduled
// by the scheduler (see client.RunJobDetached / client.RunJobAttached)
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
	if release.ImageArtifactID() == "" {
		httphelper.ValidationError(w, "release.ImageArtifact", "must be set")
		return
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
	env := make(map[string]string, len(release.Env)+len(newJob.Env)+4)
	env["FLYNN_APP_ID"] = app.ID
	env["FLYNN_RELEASE_ID"] = release.ID
	env["FLYNN_PROCESS_TYPE"] = ""
	env["FLYNN_JOB_ID"] = id
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
			Cmd:        newJob.Cmd,
			Env:        env,
			TTY:        newJob.TTY,
			Stdin:      attach,
			DisableLog: newJob.DisableLog,
		},
		Resources: newJob.Resources,
	}
	resource.SetDefaults(&job.Resources)
	if len(newJob.Entrypoint) > 0 {
		job.Config.Entrypoint = newJob.Entrypoint
	}
	if len(release.ArtifactIDs) > 0 {
		artifacts, err := c.artifactRepo.ListIDs(release.ArtifactIDs...)
		if err != nil {
			respondWithError(w, err)
			return
		}
		job.ImageArtifact = artifacts[release.ImageArtifactID()].HostArtifact()
		job.FileArtifacts = make([]*host.Artifact, len(release.FileArtifactIDs()))
		for i, id := range release.FileArtifactIDs() {
			job.FileArtifacts[i] = artifacts[id].HostArtifact()
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

	if err := client.AddJob(job); err != nil {
		respondWithError(w, fmt.Errorf("schedule failed: %s", err.Error()))
		return
	}

	if attach {
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
		<-done
		<-done

		return
	} else {
		httphelper.JSON(w, 200, &ct.Job{
			ID:        job.ID,
			UUID:      uuid,
			HostID:    hostID,
			ReleaseID: newJob.ReleaseID,
			Cmd:       newJob.Cmd,
		})
	}
}
