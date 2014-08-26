package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

type JobRepo struct {
	db *DB
}

func NewJobRepo(db *DB) *JobRepo {
	return &JobRepo{db}
}

func (r *JobRepo) Add(job *ct.Job) error {
	hostID, jobID := parseJobID(job.ID)
	if hostID == "" {
		log.Printf("Unable to parse hostID from %q", job.ID)
		return ErrNotFound
	}
	// TODO: actually validate
	err := r.db.QueryRow("INSERT INTO job_cache (job_id, host_id, app_id, release_id, process_type, state) VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at, updated_at",
		jobID, hostID, job.AppID, job.ReleaseID, job.Type, job.State).Scan(&job.CreatedAt, &job.UpdatedAt)
	if e, ok := err.(*pq.Error); ok && e.Code.Name() == "unique_violation" {
		err = r.db.QueryRow("UPDATE job_cache SET state = $3, updated_at = now() WHERE job_id = $1 AND host_id = $2 RETURNING created_at, updated_at",
			jobID, hostID, job.State).Scan(&job.CreatedAt, &job.UpdatedAt)
	}
	if err != nil {
		return err
	}
	return nil
}

func scanJob(s Scanner) (*ct.Job, error) {
	job := &ct.Job{}
	err := s.Scan(&job.ID, &job.AppID, &job.ReleaseID, &job.Type, &job.State, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	job.AppID = cleanUUID(job.AppID)
	job.ReleaseID = cleanUUID(job.ReleaseID)
	return job, nil
}

func (r *JobRepo) List(appID string) ([]*ct.Job, error) {
	rows, err := r.db.Query("SELECT concat(host_id, '-', job_id), app_id, release_id, process_type, state, created_at, updated_at FROM job_cache WHERE app_id = $1 ORDER BY created_at DESC", appID)
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

type clusterClient interface {
	ListHosts() (map[string]host.Host, error)
	DialHost(string) (cluster.Host, error)
	AddJobs(*host.AddJobsReq) (*host.AddJobsRes, error)
}

func listJobs(app *ct.App, repo *JobRepo, r ResponseHelper) {
	list, err := repo.List(app.ID)
	if err != nil {
		r.Error(err)
		return
	}
	r.JSON(200, list)
}

func putJob(job ct.Job, app *ct.App, repo *JobRepo, r ResponseHelper) {
	job.AppID = app.ID
	if err := repo.Add(&job); err != nil {
		r.Error(err)
		return
	}
	r.JSON(200, &job)
}

func jobLog(req *http.Request, app *ct.App, params martini.Params, hc cluster.Host, w http.ResponseWriter, r ResponseHelper) {
	attachReq := &host.AttachReq{
		JobID: params["jobs_id"],
		Flags: host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagLogs,
	}
	tail := req.FormValue("tail") != ""
	if tail {
		attachReq.Flags |= host.AttachFlagStream
	}
	wait := req.FormValue("wait") != ""
	attachClient, err := hc.Attach(attachReq, wait)
	if err != nil {
		if err == cluster.ErrWouldWait {
			w.WriteHeader(404)
		} else {
			r.Error(err)
		}
		return
	}

	if cn, ok := w.(http.CloseNotifier); ok {
		go func() {
			<-cn.CloseNotify()
			attachClient.Close()
		}()
	} else {
		defer attachClient.Close()
	}

	sse := strings.Contains(req.Header.Get("Accept"), "text/event-stream")
	if sse {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/vnd.flynn.attach")
	}
	w.WriteHeader(200)
	// Send headers right away if tailing
	if wf, ok := w.(http.Flusher); ok && tail {
		wf.Flush()
	}

	fw := flushWriter{w, tail}
	if sse {
		ssew := NewSSELogWriter(w)
		exit, err := attachClient.Receive(flushWriter{ssew.Stream("stdout"), tail}, flushWriter{ssew.Stream("stderr"), tail})
		if err != nil {
			fw.Write([]byte("event: error\ndata: {}\n\n"))
			return
		}
		if tail {
			fmt.Fprintf(fw, "event: exit\ndata: {\"status\": %d}\n\n", exit)
			return
		}
		fw.Write([]byte("event: eof\ndata: {}\n\n"))
	} else {
		io.Copy(fw, attachClient.Conn())
	}
}

type SSELogWriter interface {
	Stream(string) io.Writer
}

func NewSSELogWriter(w io.Writer) SSELogWriter {
	return &sseLogWriter{Writer: w, Encoder: json.NewEncoder(w)}
}

type sseLogWriter struct {
	io.Writer
	*json.Encoder
	sync.Mutex
}

func (w *sseLogWriter) Stream(s string) io.Writer {
	return &sseLogStreamWriter{w: w, s: s}
}

type sseLogStreamWriter struct {
	w *sseLogWriter
	s string
}

type sseLogChunk struct {
	Stream string `json:"stream"`
	Data   string `json:"data"`
}

func (w *sseLogStreamWriter) Write(p []byte) (int, error) {
	w.w.Lock()
	defer w.w.Unlock()

	if _, err := w.w.Write([]byte("data: ")); err != nil {
		return 0, err
	}
	if err := w.w.Encode(&sseLogChunk{Stream: w.s, Data: string(p)}); err != nil {
		return 0, err
	}
	_, err := w.w.Write([]byte("\n"))
	return len(p), err
}

func (w *sseLogStreamWriter) Flush() {
	if fw, ok := w.w.Writer.(http.Flusher); ok {
		fw.Flush()
	}
}

type flushWriter struct {
	w  io.Writer
	ok bool
}

func (f flushWriter) Write(p []byte) (int, error) {
	if f.ok {
		defer func() {
			if fw, ok := f.w.(http.Flusher); ok {
				fw.Flush()
			}
		}()
	}
	return f.w.Write(p)
}

func parseJobID(jobID string) (string, string) {
	id := strings.SplitN(jobID, "-", 2)
	if len(id) != 2 || id[0] == "" || id[1] == "" {
		return "", ""
	}
	return id[0], id[1]
}

func connectHostMiddleware(c martini.Context, params martini.Params, cl clusterClient, r ResponseHelper) {
	hostID, jobID := parseJobID(params["jobs_id"])
	if hostID == "" {
		log.Printf("Unable to parse hostID from %q", params["jobs_id"])
		r.Error(ErrNotFound)
		return
	}
	params["jobs_id"] = jobID

	client, err := cl.DialHost(hostID)
	if err != nil {
		r.Error(err)
		return
	}
	c.MapTo(client, (*cluster.Host)(nil))

	c.Next()
	client.Close()
}

func killJob(app *ct.App, params martini.Params, client cluster.Host, r ResponseHelper) {
	if err := client.StopJob(params["jobs_id"]); err != nil {
		r.Error(err)
		return
	}
}

func runJob(app *ct.App, newJob ct.NewJob, releases *ReleaseRepo, artifacts *ArtifactRepo, cl clusterClient, req *http.Request, w http.ResponseWriter, r ResponseHelper) {
	data, err := releases.Get(newJob.ReleaseID)
	if err != nil {
		r.Error(err)
		return
	}
	release := data.(*ct.Release)
	data, err = artifacts.Get(release.ArtifactID)
	if err != nil {
		r.Error(err)
		return
	}
	artifact := data.(*ct.Artifact)
	attach := strings.Contains(req.Header.Get("Accept"), "application/vnd.flynn.attach")

	env := make(map[string]string, len(release.Env)+len(newJob.Env))
	for k, v := range release.Env {
		env[k] = v
	}
	for k, v := range newJob.Env {
		env[k] = v
	}
	job := &host.Job{
		ID: cluster.RandomJobID(""),
		Metadata: map[string]string{
			"flynn-controller.app":     app.ID,
			"flynn-controller.release": release.ID,
		},
		Artifact: host.Artifact{
			Type: artifact.Type,
			URI:  artifact.URI,
		},
		Config: host.ContainerConfig{
			Cmd:   newJob.Cmd,
			Env:   env,
			TTY:   newJob.TTY,
			Stdin: attach,
		},
	}

	hosts, err := cl.ListHosts()
	if err != nil {
		r.Error(err)
		return
	}
	// pick a random host
	var hostID string
	for hostID = range hosts {
		break
	}
	if hostID == "" {
		r.Error(errors.New("no hosts found"))
		return
	}

	var attachClient cluster.AttachClient
	if attach {
		attachReq := &host.AttachReq{
			JobID:  job.ID,
			Flags:  host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagStdin | host.AttachFlagStream,
			Height: uint16(newJob.Lines),
			Width:  uint16(newJob.Columns),
		}
		client, err := cl.DialHost(hostID)
		if err != nil {
			r.Error(fmt.Errorf("host connect failed: %s", err.Error()))
			return
		}
		defer client.Close()
		attachClient, err = client.Attach(attachReq, true)
		if err != nil {
			r.Error(fmt.Errorf("attach failed: %s", err.Error()))
			return
		}
		defer attachClient.Close()
	}

	_, err = cl.AddJobs(&host.AddJobsReq{HostJobs: map[string][]*host.Job{hostID: {job}}})
	if err != nil {
		r.Error(fmt.Errorf("schedule failed: %s", err.Error()))
		return
	}

	if attach {
		if err := attachClient.Wait(); err != nil {
			r.Error(fmt.Errorf("attach wait failed: %s", err.Error()))
			return
		}
		w.Header().Set("Content-Type", "application/vnd.flynn.attach")
		w.Header().Set("Content-Length", "0")
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
		r.JSON(200, &ct.Job{
			ID:        hostID + "-" + job.ID,
			ReleaseID: newJob.ReleaseID,
			Cmd:       newJob.Cmd,
		})
	}
}
