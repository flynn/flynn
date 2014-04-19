package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-controller/utils"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-dockerclient"
	"github.com/flynn/go-flynn/cluster"
	"github.com/flynn/go-flynn/demultiplex"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/render"
)

type clusterClient interface {
	ListHosts() (map[string]host.Host, error)
	DialHost(string) (cluster.Host, error)
	AddJobs(*host.AddJobsReq) (*host.AddJobsRes, error)
}

func jobList(app *ct.App, cc clusterClient, r render.Render) {
	hosts, err := cc.ListHosts()
	if err != nil {
		log.Println(err)
		r.JSON(500, struct{}{})
		return
	}
	var jobs []ct.Job
	for _, h := range hosts {
		for _, j := range h.Jobs {
			if j.Attributes["flynn-controller.app"] != app.ID {
				continue
			}

			job := ct.Job{
				ID:        h.ID + "-" + j.ID,
				Type:      j.Attributes["flynn-controller.type"],
				ReleaseID: j.Attributes["flynn-controller.release"],
			}
			if job.Type == "" {
				job.Cmd = j.Config.Cmd
			}
			jobs = append(jobs, job)
		}
	}

	r.JSON(200, jobs)
}

func jobLog(req *http.Request, app *ct.App, params martini.Params, cluster cluster.Host, w http.ResponseWriter) {
	attachReq := &host.AttachReq{
		JobID: params["jobs_id"],
		Flags: host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagLogs,
	}
	if tail := req.FormValue("tail"); tail != "" {
		attachReq.Flags |= host.AttachFlagStream
	}
	stream, _, err := cluster.Attach(attachReq, false)
	if err != nil {
		// TODO: handle AttachWouldWait
		log.Println(err)
		w.WriteHeader(500)
		return
	}
	defer stream.Close()
	if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		ssew := NewSSELogWriter(w)
		demultiplex.Copy(ssew.Stream("stdout"), ssew.Stream("stderr"), stream)
		// TODO: include exit code here if tailing
		w.Write([]byte("event: eof\ndata: {}\n\n"))
	} else {
		io.Copy(w, stream)
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

func parseJobID(params martini.Params) (string, string) {
	id := strings.SplitN(params["jobs_id"], "-", 2)
	if len(id) != 2 || id[0] == "" || id[1] == "" {
		return "", ""
	}
	return id[0], id[1]
}

func connectHostMiddleware(c martini.Context, params martini.Params, cl clusterClient, w http.ResponseWriter) {
	hostID, jobID := parseJobID(params)
	if hostID == "" {
		log.Printf("Unable to parse hostID from %q", params["jobs_id"])
		w.WriteHeader(404)
		return
	}
	params["jobs_id"] = jobID

	client, err := cl.DialHost(hostID)
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}
	c.MapTo(client, (*cluster.Host)(nil))

	c.Next()
	client.Close()
}

func killJob(app *ct.App, params martini.Params, client cluster.Host, w http.ResponseWriter) {
	if err := client.StopJob(params["jobs_id"]); err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}
}

func runJob(app *ct.App, newJob ct.NewJob, releases *ReleaseRepo, artifacts *ArtifactRepo, cl clusterClient, req *http.Request, w http.ResponseWriter, r render.Render) {
	data, err := releases.Get(newJob.ReleaseID)
	if err != nil {
		// TODO: 400 on ErrNotFound
		log.Println("error getting release", err)
		w.WriteHeader(500)
		return
	}
	release := data.(*ct.Release)
	data, err = artifacts.Get(release.ArtifactID)
	if err != nil {
		// TODO: 400 on ErrNotFound
		log.Println("error getting artifact", err)
		w.WriteHeader(500)
		return
	}
	artifact := data.(*ct.Artifact)
	image, err := utils.DockerImage(artifact.URI)
	if err != nil {
		log.Println("error parsing artifact uri", err)
		w.WriteHeader(400)
		return
	}
	attach := strings.Contains(req.Header.Get("Accept"), "application/vnd.flynn.attach")

	job := &host.Job{
		ID: cluster.RandomJobID(""),
		Attributes: map[string]string{
			"flynn-controller.app":     app.ID,
			"flynn-controller.release": release.ID,
		},
		Config: &docker.Config{
			Cmd:          newJob.Cmd,
			Env:          utils.FormatEnv(release.Env, newJob.Env),
			Image:        image,
			AttachStdout: true,
			AttachStderr: true,
		},
	}
	if newJob.TTY {
		job.Config.Tty = true
	}
	if attach {
		job.Config.AttachStdin = true
		job.Config.StdinOnce = true
		job.Config.OpenStdin = true
	}

	hosts, err := cl.ListHosts()
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}
	// pick a random host
	var hostID string
	for hostID = range hosts {
		break
	}
	if hostID == "" {
		log.Println("no hosts found")
		w.WriteHeader(500)
		return
	}

	var attachConn cluster.ReadWriteCloser
	var attachWait func() error
	if attach {
		attachReq := &host.AttachReq{
			JobID:  job.ID,
			Flags:  host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagStdin | host.AttachFlagStream,
			Height: newJob.Lines,
			Width:  newJob.Columns,
		}
		client, err := cl.DialHost(hostID)
		if err != nil {
			w.WriteHeader(500)
			log.Println("lorne connect failed", err)
			return
		}
		defer client.Close()
		attachConn, attachWait, err = client.Attach(attachReq, true)
		if err != nil {
			w.WriteHeader(500)
			log.Println("attach failed", err)
			return
		}
		defer attachConn.Close()
	}

	_, err = cl.AddJobs(&host.AddJobsReq{HostJobs: map[string][]*host.Job{hostID: {job}}})
	if err != nil {
		log.Println("schedule failed", err)
		w.WriteHeader(500)
		return
	}

	if attach {
		if err := attachWait(); err != nil {
			log.Println("attach wait failed", err)
			w.WriteHeader(500)
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
		cp := func(to cluster.ReadWriteCloser, from io.Reader) {
			io.Copy(to, from)
			to.CloseWrite()
			done <- struct{}{}
		}
		go cp(conn.(cluster.ReadWriteCloser), attachConn)
		go cp(attachConn, conn)
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
