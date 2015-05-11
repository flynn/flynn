package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq/hstore"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/schedutil"
	"github.com/flynn/flynn/pkg/sse"
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
	row := r.db.QueryRow("SELECT concat(host_id, '-', job_id), app_id, release_id, process_type, state, meta, created_at, updated_at FROM job_cache WHERE concat(host_id, '-', job_id) = $1", id)
	return scanJob(row)
}

func (r *JobRepo) Add(job *ct.Job) error {
	hostID, jobID, err := cluster.ParseJobID(job.ID)
	if err != nil {
		log.Printf("Unable to parse hostID from %q", job.ID)
		return ErrNotFound
	}
	meta := metaToHstore(job.Meta)
	// TODO: actually validate
	err = r.db.QueryRow("INSERT INTO job_cache (job_id, host_id, app_id, release_id, process_type, state, meta) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING created_at, updated_at",
		jobID, hostID, job.AppID, job.ReleaseID, job.Type, job.State, meta).Scan(&job.CreatedAt, &job.UpdatedAt)
	if postgres.IsUniquenessError(err, "") {
		err = r.db.QueryRow("UPDATE job_cache SET state = $3, updated_at = now() WHERE job_id = $1 AND host_id = $2 RETURNING created_at, updated_at",
			jobID, hostID, job.State).Scan(&job.CreatedAt, &job.UpdatedAt)
		if e, ok := err.(*pq.Error); ok && e.Code.Name() == "check_violation" {
			return ct.ValidationError{Field: "state", Message: e.Error()}
		}
	}
	if err != nil {
		return err
	}

	// create a job event, ignoring possible duplications
	err = r.db.Exec("INSERT INTO job_events (job_id, host_id, app_id, state) VALUES ($1, $2, $3, $4)", jobID, hostID, job.AppID, job.State)
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
	rows, err := r.db.Query("SELECT concat(host_id, '-', job_id), app_id, release_id, process_type, state, meta, created_at, updated_at FROM job_cache WHERE app_id = $1 ORDER BY created_at DESC", appID)
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

func (r *JobRepo) listEvents(appID string, sinceID int64, count int) ([]*ct.JobEvent, error) {
	query := "SELECT event_id, concat(job_events.host_id, '-', job_events.job_id), job_events.app_id, job_cache.release_id, job_cache.process_type, job_events.state, job_events.created_at FROM job_events INNER JOIN job_cache ON job_events.job_id = job_cache.job_id AND job_events.host_id = job_cache.host_id WHERE job_events.app_id = $1 AND event_id > $2 ORDER BY event_id DESC"
	args := []interface{}{appID, sinceID}
	if count > 0 {
		query += " LIMIT $3"
		args = append(args, count)
	}
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	var events []*ct.JobEvent
	for rows.Next() {
		event, err := scanJobEvent(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (r *JobRepo) getEvent(eventID int64) (*ct.JobEvent, error) {
	row := r.db.QueryRow("SELECT event_id, concat(job_events.host_id, '-', job_events.job_id), job_events.app_id, job_cache.release_id, job_cache.process_type, job_events.state, job_events.created_at FROM job_events INNER JOIN job_cache ON job_events.job_id = job_cache.job_id AND job_events.host_id = job_cache.host_id WHERE job_events.event_id = $1", eventID)
	return scanJobEvent(row)
}

func scanJobEvent(s postgres.Scanner) (*ct.JobEvent, error) {
	event := &ct.JobEvent{}
	err := s.Scan(&event.ID, &event.JobID, &event.AppID, &event.ReleaseID, &event.Type, &event.State, &event.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	event.AppID = postgres.CleanUUID(event.AppID)
	event.ReleaseID = postgres.CleanUUID(event.ReleaseID)
	return event, nil
}

type clusterClient interface {
	ListHosts() ([]host.Host, error)
	DialHost(string) (cluster.Host, error)
	AddJobs(map[string][]*host.Job) (map[string]host.Host, error)
}

func (c *controllerAPI) connectHost(ctx context.Context) (cluster.Host, string, error) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	hostID, jobID, err := cluster.ParseJobID(params.ByName("jobs_id"))
	if err != nil {
		log.Printf("Unable to parse hostID from %q", params.ByName("jobs_id"))
		return nil, jobID, err
	}

	client, err := c.clusterClient.DialHost(hostID)
	if err != nil {
		return nil, jobID, err
	}
	return client, jobID, nil
}

func (c *controllerAPI) ListJobs(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	app := c.getApp(ctx)
	if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		if err := streamJobs(ctx, req, w, app, c.jobRepo); err != nil {
			respondWithError(w, err)
		}
		return
	}
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

func streamJobs(ctx context.Context, req *http.Request, w http.ResponseWriter, app *ct.App, repo *JobRepo) (err error) {
	var lastID int64
	if req.Header.Get("Last-Event-Id") != "" {
		lastID, err = strconv.ParseInt(req.Header.Get("Last-Event-Id"), 10, 64)
		if err != nil {
			return ct.ValidationError{Field: "Last-Event-Id", Message: "is invalid"}
		}
	}
	var count int
	if req.FormValue("count") != "" {
		count, err = strconv.Atoi(req.FormValue("count"))
		if err != nil {
			return ct.ValidationError{Field: "count", Message: "is invalid"}
		}
	}

	ch := make(chan *ct.JobEvent)
	l, _ := ctxhelper.LoggerFromContext(ctx)
	log := l.New("fn", "streamJobs", "app_id", app.ID)
	s := sse.NewStream(w, ch, log)
	s.Serve()
	defer func() {
		if err == nil {
			s.Close()
		} else {
			s.CloseWithError(err)
		}
	}()

	listener, err := repo.db.Listen("job_events:"+postgres.FormatUUID(app.ID), log)
	if err != nil {
		return err
	}
	defer listener.Close()

	var currID int64
	if lastID > 0 || count > 0 {
		events, err := repo.listEvents(app.ID, lastID, count)
		if err != nil {
			return err
		}
		// events are in ID DESC order, so iterate in reverse
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			ch <- e
			currID = e.ID
		}
	}

	for {
		select {
		case <-s.Done:
			return
		case n, ok := <-listener.Notify:
			if !ok {
				return listener.Err
			}
			id, err := strconv.ParseInt(n.Extra, 10, 64)
			if err != nil {
				return err
			}
			if id <= currID {
				continue
			}
			e, err := repo.getEvent(id)
			if err != nil {
				return err
			}
			ch <- e
		}
	}
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

	hosts, err := c.clusterClient.ListHosts()
	if err != nil {
		respondWithError(w, err)
		return
	}
	if len(hosts) == 0 {
		respondWithError(w, errors.New("no hosts found"))
		return
	}
	hostID := schedutil.PickHost(hosts).ID

	id := cluster.RandomJobID("")
	app := c.getApp(ctx)
	env := make(map[string]string, len(release.Env)+len(newJob.Env)+4)
	env["FLYNN_APP_ID"] = app.ID
	env["FLYNN_RELEASE_ID"] = release.ID
	env["FLYNN_PROCESS_TYPE"] = ""
	env["FLYNN_JOB_ID"] = hostID + "-" + id
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
		client, err := c.clusterClient.DialHost(hostID)
		if err != nil {
			respondWithError(w, fmt.Errorf("host connect failed: %s", err.Error()))
			return
		}
		attachClient, err = client.Attach(attachReq, true)
		if err != nil {
			respondWithError(w, fmt.Errorf("attach failed: %s", err.Error()))
			return
		}
		defer attachClient.Close()
	}

	_, err = c.clusterClient.AddJobs(map[string][]*host.Job{hostID: {job}})
	if err != nil {
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
			ID:        hostID + "-" + job.ID,
			ReleaseID: newJob.ReleaseID,
			Cmd:       newJob.Cmd,
		})
	}
}
