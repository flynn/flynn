package utils

import (
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

func JobConfig(f *ct.ExpandedFormation, name string) *host.Job {
	t := f.Release.Processes[name]
	env := make(map[string]string, len(f.Release.Env)+len(t.Env)+4)
	for k, v := range f.Release.Env {
		env[k] = v
	}
	for k, v := range t.Env {
		env[k] = v
	}
	id := cluster.RandomJobID("")
	env["FLYNN_APP_ID"] = f.App.ID
	env["FLYNN_RELEASE_ID"] = f.Release.ID
	env["FLYNN_PROCESS_TYPE"] = name
	env["FLYNN_JOB_ID"] = id
	job := &host.Job{
		ID: id,
		Metadata: map[string]string{
			"flynn-controller.app":      f.App.ID,
			"flynn-controller.app_name": f.App.Name,
			"flynn-controller.release":  f.Release.ID,
			"flynn-controller.type":     name,
		},
		Artifact: host.Artifact{
			Type: f.Artifact.Type,
			URI:  f.Artifact.URI,
		},
		Config: host.ContainerConfig{
			Cmd:         t.Cmd,
			Env:         env,
			HostNetwork: t.HostNetwork,
		},
	}
	if len(t.Entrypoint) > 0 {
		job.Config.Entrypoint = t.Entrypoint
	}
	job.Config.Ports = make([]host.Port, len(t.Ports))
	for i, p := range t.Ports {
		job.Config.Ports[i].Proto = p.Proto
		job.Config.Ports[i].Port = p.Port
		job.Config.Ports[i].Service = p.Service
	}
	if t.Data {
		job.Config.Mounts = []host.Mount{{Location: "/data", Writeable: true}}
	}
	return job
}
