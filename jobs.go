package main

import (
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/codegangsta/martini"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-controller/utils"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-dockerclient"
	"github.com/flynn/go-flynn/cluster"
	"github.com/martini-contrib/render"
)

type clusterClient interface {
	ListHosts() (map[string]host.Host, error)
	ConnectHost(string) (cluster.Host, error)
	AddJobs(*host.AddJobsReq) (*host.AddJobsRes, error)
}

func jobList(app *ct.App, cc clusterClient, r render.Render) {
	hosts, err := cc.ListHosts()
	if err != nil {
		// TODO: 500/handle error
	}
	var jobs []ct.Job
	for _, h := range hosts {
		for _, j := range h.Jobs {
			if j.Attributes["flynn-controller.app"] != app.ID {
				continue
			}

			job := ct.Job{
				ID:        h.ID + ":" + j.ID,
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

func jobLog(app *ct.App, params martini.Params, cluster cluster.Host, w http.ResponseWriter) {
	attachReq := &host.AttachReq{
		JobID: params["job_id"],
		Flags: host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagLogs,
	}
	if _, ok := params["tail"]; ok {
		attachReq.Flags |= host.AttachFlagStream
	}
	stream, _, err := cluster.Attach(attachReq, false)
	if err != nil {
		// TODO: 500/log error
		// TODO: handle AttachWouldWait
	}
	defer stream.Close()
	io.Copy(w, stream)
}

func parseJobID(params martini.Params) (string, string) {
	id := strings.SplitN(params["job_id"], ":", 2)
	if len(id) != 2 || id[0] == "" || id[1] == "" {
		return "", ""
	}
	return id[0], id[1]
}

func connectHostMiddleware(c martini.Context, params martini.Params, cl clusterClient) {
	hostID, jobID := parseJobID(params)
	if hostID == "" {
		// TODO: error
	}
	params["job_id"] = jobID

	client, err := cl.ConnectHost(hostID)
	if err != nil {
		// TODO: 500/log error
	}
	c.MapTo(client, (*cluster.Host)(nil))

	c.Next()
	client.Close()
}

func killJob(app *ct.App, params martini.Params, client cluster.Host) {
	if err := client.StopJob(params["job_id"]); err != nil {
		// TODO: 500/log error
	}

}

func runJob(app *ct.App, newJob ct.NewJob, releases *ReleaseRepo, artifacts *ArtifactRepo, cl clusterClient, req *http.Request, w http.ResponseWriter) {
	r, err := releases.Get(newJob.ReleaseID)
	if err != nil {
		// TODO: 400 on ErrNotFound
		log.Println("error getting release", err)
		w.WriteHeader(500)
		return
	}
	release := r.(*ct.Release)
	a, err := artifacts.Get(release.ArtifactID)
	if err != nil {
		// TODO: 400 on ErrNotFound
		log.Println("error getting artifact", err)
		w.WriteHeader(500)
		return
	}
	artifact := a.(*ct.Artifact)
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
		client, err := cl.ConnectHost(hostID)
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

	res, err := cl.AddJobs(&host.AddJobsReq{HostJobs: map[string][]*host.Job{hostID: {job}}})
	if err != nil || !res.Success {
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
		w.WriteHeader(200)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			panic(err)
		}
		defer conn.Close()

		// TODO: demux stdout/stderr if non-tty

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
	}
}
