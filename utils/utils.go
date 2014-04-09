package utils

import (
	"errors"
	"net/url"
	"strings"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-dockerclient"
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

func JobConfig(f *ct.ExpandedFormation, name string) (*host.Job, error) {
	t := f.Release.Processes[name]
	image, err := DockerImage(f.Artifact.URI)
	if err != nil {
		return nil, err
	}
	job := &host.Job{
		TCPPorts: t.Ports.TCP,
		Attributes: map[string]string{
			"flynn-controller.app":     f.App.ID,
			"flynn-controller.release": f.Release.ID,
			"flynn-controller.type":    name,
		},
		Config: &docker.Config{
			Cmd: t.Cmd,
			Env: FormatEnv(f.Release.Env, t.Env,
				map[string]string{
					"FLYNN_APP_ID":     f.App.ID,
					"FLYNN_RELEASE_ID": f.Release.ID,
				},
			),
			Image: image,
		},
	}
	if t.Data {
		job.Config.Volumes = map[string]struct{}{"/data": {}}
	}
	if p := t.Env["FLYNN_HOST_PORTS"]; p != "" {
		ports := strings.Split(p, ",")
		job.HostConfig = &docker.HostConfig{
			PortBindings:    make(map[string][]docker.PortBinding, len(ports)),
			PublishAllPorts: true,
		}
		job.Config.ExposedPorts = make(map[string]struct{}, len(ports))
		for _, port := range ports {
			job.Config.ExposedPorts[port+"/tcp"] = struct{}{}
			job.HostConfig.PortBindings[port+"/tcp"] = []docker.PortBinding{{HostPort: port}}
		}
	}
	return job, nil
}
