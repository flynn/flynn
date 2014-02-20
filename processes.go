package main

import (
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

func processList(app *ct.App, cc clusterClient, r render.Render) {
	hosts, err := cc.ListHosts()
	if err != nil {
		// TODO: 500/handle error
	}
	var processes []ct.Process
	for _, h := range hosts {
		for _, job := range h.Jobs {
			if job.Attributes["flynn-controller.app"] != app.ID {
				continue
			}

			proc := ct.Process{
				ID:        h.ID + ":" + job.ID,
				Type:      job.Attributes["flynn-controller.type"],
				ReleaseID: job.Attributes["flynn-controller.release"],
			}
			if proc.Type == "" {
				proc.Cmd = job.Config.Cmd
			}
			processes = append(processes, proc)
		}
	}

	r.JSON(200, processes)
}

func parseProcessID(params martini.Params) (string, string) {
	id := strings.SplitN(params["proc_id"], ":", 2)
	if len(id) != 2 || id[0] == "" || id[1] == "" {
		return "", ""
	}
	return id[0], id[1]
}

func connectHostMiddleware(c martini.Context, params martini.Params, cl clusterClient) {
	hostID, jobID := parseProcessID(params)
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

func killProcess(app *ct.App, params martini.Params, client cluster.Host) {
	if err := client.StopJob(params["job_id"]); err != nil {
		// TODO: 500/log error
	}
}
