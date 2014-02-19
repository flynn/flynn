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

func killProcess(app *ct.App, params martini.Params, cl clusterClient) {
	id := strings.SplitN(params["proc_id"], ":", 2)
	if len(id) != 2 {
		// TODO: error
	}
	client, err := cl.ConnectHost(id[0])
	if err != nil {
		// TODO: 500/log error
	}
	if err := client.StopJob(id[1]); err != nil {
		// TODO: 500/log error
	}
}
