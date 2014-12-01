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
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/sse"
)

type JobRepo struct {
	db *DB
}

func NewJobRepo(db *DB) *JobRepo {
	return &JobRepo{db}
}

func (r *JobRepo) Get(id string) (*ct.Job, error) {
	row := r.db.QueryRow("SELECT concat(host_id, '-', job_id), app_id, release_id, process_type, state, created_at, updated_at FROM job_cache WHERE concat(host_id, '-', job_id) = $1", id)
	return scanJob(row)
}

func (r *JobRepo) Add(job *ct.Job) error {
	hostID, jobID, err := cluster.ParseJobID(job.ID)
	if err != nil {
		log.Printf("Unable to parse hostID from %q", job.ID)
		return ErrNotFound
	}
	// TODO: actually validate
	err = r.db.QueryRow("INSERT INTO job_cache (job_id, host_id, app_id, release_id, process_type, state) VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at, updated_at",
		jobID, hostID, job.AppID, job.ReleaseID, job.Type, job.State).Scan(&job.CreatedAt, &job.UpdatedAt)
	if e, ok := err.(*pq.Error); ok && e.Code.Name() == "unique_violation" {
		err = r.db.QueryRow("UPDATE job_cache SET state = $3, updated_at = now() WHERE job_id = $1 AND host_id = $2 RETURNING created_at, updated_at",
			jobID, hostID, job.State).Scan(&job.CreatedAt, &job.UpdatedAt)
	}
	if err != nil {
		return err
	}
	return r.db.Exec("INSERT INTO job_events (job_id, host_id, app_id, state) VALUES ($1, $2, $3, $4)", jobID, hostID, job.AppID, job.State)
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

func scanJobEvent(s Scanner) (*ct.JobEvent, error) {
	event := &ct.JobEvent{}
	err := s.Scan(&event.ID, &event.JobID, &event.AppID, &event.ReleaseID, &event.Type, &event.State, &event.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	event.AppID = cleanUUID(event.AppID)
	event.ReleaseID = cleanUUID(event.ReleaseID)
	return event, nil
}

type clusterClient interface {
	ListHosts() (map[string]host.Host, error)
	DialHost(string) (cluster.Host, error)
	AddJobs(*host.AddJobsReq) (*host.AddJobsRes, error)
}

func listJobs(req *http.Request, w http.ResponseWriter, app *ct.App, repo *JobRepo, r ResponseHelper) {
	if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		if err := streamJobs(req, w, app, repo); err != nil {
			r.Error(err)
		}
		return
	}
	list, err := repo.List(app.ID)
	if err != nil {
		r.Error(err)
		return
	}
	r.JSON(200, list)
}

func getJob(params martini.Params, app *ct.App, repo *JobRepo, r ResponseHelper) {
	job, err := repo.Get(params["jobs_id"])
	if err != nil {
		r.Error(err)
		return
	}
	r.JSON(200, job)
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

func streamJobs(req *http.Request, w http.ResponseWriter, app *ct.App, repo *JobRepo) (err error) {
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

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")

	sendKeepAlive := func() error {
		if _, err := w.Write([]byte(":\n")); err != nil {
			return err
		}
		w.(http.Flusher).Flush()
		return nil
	}

	sendJobEvent := func(e *ct.JobEvent) error {
		if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: ", e.ID, e.State); err != nil {
			return err
		}
		if err := json.NewEncoder(w).Encode(e); err != nil {
			return err
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return err
		}
		w.(http.Flusher).Flush()
		return nil
	}

	connected := make(chan struct{})
	done := make(chan struct{})
	listenEvent := func(ev pq.ListenerEventType, listenErr error) {
		switch ev {
		case pq.ListenerEventConnected:
			close(connected)
		case pq.ListenerEventDisconnected:
			close(done)
		case pq.ListenerEventConnectionAttemptFailed:
			err = listenErr
			close(done)
		}
	}
	listener := pq.NewListener(repo.db.DSN(), 10*time.Second, time.Minute, listenEvent)
	defer listener.Close()
	listener.Listen("job_events:" + formatUUID(app.ID))

	var currID int64
	if lastID > 0 || count > 0 {
		events, err := repo.listEvents(app.ID, lastID, count)
		if err != nil {
			return err
		}
		// events are in ID DESC order, so iterate in reverse
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			if err := sendJobEvent(e); err != nil {
				return err
			}
			currID = e.ID
		}
	}

	select {
	case <-done:
		return
	case <-connected:
	}

	if err = sendKeepAlive(); err != nil {
		return
	}

	closed := w.(http.CloseNotifier).CloseNotify()
	for {
		select {
		case <-done:
			return
		case <-closed:
			return
		case <-time.After(30 * time.Second):
			if err := sendKeepAlive(); err != nil {
				return err
			}
		case n := <-listener.Notify:
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
			if err = sendJobEvent(e); err != nil {
				return err
			}
		}
	}
}

type SSELogWriter interface {
	Stream(string) io.Writer
}

func NewSSELogWriter(w io.Writer) SSELogWriter {
	return &sseLogWriter{SSEWriter: &sse.Writer{Writer: w}}
}

type sseLogWriter struct {
	sse.SSEWriter
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
	data, err := json.Marshal(&sseLogChunk{Stream: w.s, Data: string(p)})
	if err != nil {
		return 0, err
	}
	if _, err := w.w.Write(data); err != nil {
		return 0, err
	}
	return len(p), err
}

func (w *sseLogStreamWriter) Flush() {
	if fw, ok := w.w.SSEWriter.(http.Flusher); ok {
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

func formatUUID(s string) string {
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

func connectHostMiddleware(c martini.Context, params martini.Params, cl clusterClient, r ResponseHelper) {
	hostID, jobID, err := cluster.ParseJobID(params["jobs_id"])
	if err != nil {
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
			"flynn-controller.app":      app.ID,
			"flynn-controller.app_name": app.Name,
			"flynn-controller.release":  release.ID,
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
	if len(newJob.Entrypoint) > 0 {
		job.Config.Entrypoint = newJob.Entrypoint
	}
	if len(newJob.Ports) > 0 {
		ports := make([]host.Port, len(newJob.Ports))
		for i, p := range newJob.Ports {
			ports[i] = host.Port{
				Port:     p.Port,
				Proto:    p.Proto,
				RangeEnd: p.RangeEnd,
			}
		}
		job.Config.Ports = ports
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
