package main

import (
	"io"
	"net/http"
	"strings"

	"github.com/codegangsta/martini"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-flynn/cluster"
	"github.com/martini-contrib/render"
)

type clusterClient interface {
	ListHosts() (map[string]host.Host, error)
	ConnectHost(string) (cluster.Host, error)
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

func jobLogs(app *ct.App, params martini.Params, cluster cluster.Host, w http.ResponseWriter) {
	attachReq := &host.AttachReq{
		JobID: params["job_id"],
		Flags: host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagStdin | host.AttachFlagLogs,
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
