package main

import (
	"github.com/codegangsta/martini-contrib/render"
	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-flynn/cluster"
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
