package utils

import (
	"errors"
	"net/url"
	"strings"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
)

func FormatEnv(envs ...map[string]string) []string {
	env := make(map[string]string)
	for _, e := range envs {
		for k, v := range e {
			env[k] = v
		}
	}
	res := make([]string, 0, len(env))
	for k, v := range env {
		res = append(res, k+"="+v)
	}
	return res
}

func DockerImage(uri string) (string, error) {
	// TODO: ID refs (see https://github.com/dotcloud/docker/issues/4106)
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme != "docker" {
		return "", errors.New("utils: only docker artifact URIs are currently supported")
	}
	if tag := u.Query().Get("tag"); tag != "" {
		u.Path += ":" + tag
	}
	if u.Host == "" {
		// docker:///foo/bar results in u.Host == ""
		u.Path = strings.TrimPrefix(u.Path, "/")
	}
	return u.Host + u.Path, nil
}

func JobConfig(f *ct.ExpandedFormation, name string) *host.Job {
	t := f.Release.Processes[name]
	env := make(map[string]string, len(f.Release.Env)+len(t.Env)+2)
	for k, v := range f.Release.Env {
		env[k] = v
	}
	for k, v := range t.Env {
		env[k] = v
	}
	env["FLYNN_APP_ID"] = f.App.ID
	env["FLYNN_RELEASE_ID"] = f.Release.ID
	job := &host.Job{
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
			Cmd: t.Cmd,
			Env: env,
		},
	}
	if len(t.Entrypoint) > 0 {
		job.Config.Entrypoint = t.Entrypoint
	}
	job.Config.Ports = make([]host.Port, len(t.Ports))
	for i, p := range t.Ports {
		job.Config.Ports[i].Proto = p.Proto
		job.Config.Ports[i].Port = p.Port
		job.Config.Ports[i].RangeEnd = p.RangeEnd
	}
	if t.Data {
		job.Config.Mounts = []host.Mount{{Location: "/data", Writeable: true}}
	}
	return job
}

func ParseJobID(jobID string) (string, string) {
	id := strings.SplitN(jobID, "-", 2)
	if len(id) != 2 || id[0] == "" || id[1] == "" {
		return "", ""
	}
	return id[0], id[1]
}
