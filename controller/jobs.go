package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq/hstore"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
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
)

/* SSE Logger */
type sseLogStream struct {
	Name string
	Chan chan<- *sseLogChunk
}

func (s *sseLogStream) Write(p []byte) (int, error) {
	data, err := json.Marshal(string(p))
	if err != nil {
		return 0, err
	}
	s.Chan <- &sseLogChunk{Event: s.Name, Data: data}
	return len(p), nil
}

type sseLogChunk struct {
	Event string          `json:"event,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

/* Job Stuff */
type JobRepo struct {
	db *postgres.DB
}

func NewJobRepo(db *postgres.DB) *JobRepo {
	return &JobRepo{db}
}

func (r *JobRepo) Get(id string) (*ct.Job, error) {
	row := r.db.QueryRow("SELECT job_id, app_id, release_id, process_type, state, meta, created_at, updated_at FROM job_cache WHERE job_id = $1", id)
	return scanJob(row)
}

func (r *JobRepo) Add(job *ct.Job) error {
	meta := metaToHstore(job.Meta)
	// TODO: actually validate
	err := r.db.QueryRow("INSERT INTO job_cache (job_id, app_id, release_id, process_type, state, meta) VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at, updated_at",
		job.ID, job.AppID, job.ReleaseID, job.Type, job.State, meta).Scan(&job.CreatedAt, &job.UpdatedAt)
	if postgres.IsUniquenessError(err, "") {
		err = r.db.QueryRow("UPDATE job_cache SET state = $2, updated_at = now() WHERE job_id = $1 RETURNING created_at, updated_at",
			job.ID, job.State).Scan(&job.CreatedAt, &job.UpdatedAt)
		if e, ok := err.(*pq.Error); ok && e.Code.Name() == "check_violation" {
			return ct.ValidationError{Field: "state", Message: e.Error()}
		}
	}
	if err != nil {
		return err
	}

	// create a job event, ignoring possible duplications
	e := ct.JobEvent{
		JobID:     job.ID,
		AppID:     job.AppID,
		ReleaseID: job.ReleaseID,
		Type:      job.Type,
		State:     job.State,
	}
	uniqueID := strings.Join([]string{e.JobID, e.State}, "|")
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	err = r.db.Exec("INSERT INTO events (app_id, object_id, unique_id, object_type, data) VALUES ($1, $2, $3, $4, $5)", e.AppID, e.JobID, uniqueID, string(ct.EventTypeJob), data)
	if postgres.IsUniquenessError(err, "") {
		return nil
	}
	return err
}

func scanJob(s postgres.Scanner) (*ct.Job, error) {
	job := &ct.Job{}
	var meta hstore.Hstore
	err := s.Scan(&job.ID, &job.AppID, &job.ReleaseID, &job.Type, &job.State, &meta, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	if len(meta.Map) > 0 {
		job.Meta = make(map[string]string, len(meta.Map))
		for k, v := range meta.Map {
			job.Meta[k] = v.String
		}
	}
	job.AppID = postgres.CleanUUID(job.AppID)
	job.ReleaseID = postgres.CleanUUID(job.ReleaseID)
	return job, nil
}

func (r *JobRepo) List(appID string) ([]*ct.Job, error) {
	rows, err := r.db.Query("SELECT job_id, app_id, release_id, process_type, state, meta, created_at, updated_at FROM job_cache WHERE app_id = $1 ORDER BY created_at DESC", appID)
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

type clusterClientWrapper struct {
	*cluster.Client
}

func (c clusterClientWrapper) Host(id string) (utils.HostClient, error) {
	return c.Client.Host(id)
}

func (c clusterClientWrapper) Hosts() ([]utils.HostClient, error) {
	hosts, err := c.Client.Hosts()
	if err != nil {
		return nil, err
	}
	res := make([]utils.HostClient, len(hosts))
	for i, h := range hosts {
		res[i] = h
	}
	return res, nil
}

type clusterClient interface {
	Host(string) (utils.HostClient, error)
	Hosts() ([]utils.HostClient, error)
}

func (c *controllerAPI) connectHost(ctx context.Context) (utils.HostClient, string, error) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	jobID := params.ByName("jobs_id")
	hostID, err := cluster.ExtractHostID(jobID)
	if err != nil {
		log.Printf("Unable to parse hostID from %q", jobID)
		return nil, jobID, err
	}

	host, err := c.clusterClient.Host(hostID)
	return host, jobID, err
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
	client, jobID, err := c.connectHost(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	if err = client.StopJob(jobID); err != nil {
		respondWithError(w, err)
		return
	}
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
	data, err = c.artifactRepo.Get(release.ArtifactID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	artifact := data.(*ct.Artifact)
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

	id := cluster.GenerateJobID(client.ID())
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
		Artifact: host.Artifact{
			Type: artifact.Type,
			URI:  artifact.URI,
		},
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
			ReleaseID: newJob.ReleaseID,
			Cmd:       newJob.Cmd,
		})
	}
}
