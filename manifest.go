package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/template"

	"github.com/flynn/flynn-host/ports"
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-dockerclient"
	"github.com/flynn/go-flynn/cluster"
)

func parseEnviron() map[string]string {
	env := os.Environ()
	res := make(map[string]string, len(env))
	for _, v := range env {
		kv := strings.SplitN(v, "=", 2)
		res[kv[0]] = kv[1]
	}
	return res
}

type ManifestData struct {
	ExternalIP string
	InternalIP string
	TCPPorts   []int
	Volumes    map[string]struct{}
	Env        map[string]string
	Services   map[string]*ManifestData

	ports    *ports.Allocator
	readonly bool
}

func (m *ManifestData) TCPPort(id int) (int, error) {
	if m.readonly {
		return 0, fmt.Errorf("lorne: invalid TCPPort(%d), ManifestData is read-only", id)
	}
	if id < len(m.TCPPorts) {
		return m.TCPPorts[id], nil
	} else if id > len(m.TCPPorts) {
		return 0, fmt.Errorf("lorne: invalid TCPPort(%d), expecting id <= %d", id, len(m.TCPPorts))
	}

	port, err := m.ports.Get()
	if err != nil {
		return 0, err
	}
	m.TCPPorts = append(m.TCPPorts, int(port))
	return int(port), nil
}

func (m *ManifestData) Volume(v string) string {
	if m.Volumes == nil {
		m.Volumes = make(map[string]struct{})
	}
	m.Volumes[v] = struct{}{}
	return v
}

type manifestRunner struct {
	env          map[string]string
	externalAddr string
	bindAddr     string
	backend      Backend
}

type manifestService struct {
	ID         string            `json:"id"`
	Image      string            `json:"image"`
	Entrypoint []string          `json:"entrypoint"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env"`
	TCPPorts   []string          `json:"tcp_ports"`
}

func dockerEnv(m map[string]string) []string {
	res := make([]string, 0, len(m))
	for k, v := range m {
		res = append(res, k+"="+v)
	}
	return res
}

func (m *manifestRunner) runManifest(r io.Reader) (map[string]*ManifestData, error) {
	var services []manifestService
	if err := json.NewDecoder(r).Decode(&services); err != nil {
		return nil, err
	}

	serviceData := make(map[string]*ManifestData, len(services))
	for _, service := range services {
		data := &ManifestData{
			Env:        parseEnviron(),
			Services:   serviceData,
			ExternalIP: m.externalAddr,
			ports:      m.backend.(*DockerBackend).ports,
		}

		// Add explicit tcp ports to data.TCPPorts
		for _, port := range service.TCPPorts {
			port, err := strconv.Atoi(port)
			if err != nil {
				return nil, err
			}
			data.TCPPorts = append(data.TCPPorts, port)
		}

		var buf bytes.Buffer

		interp := func(s string) (string, error) {
			t, err := template.New("arg").Parse(s)
			if err != nil {
				return "", err
			}
			if err := t.Execute(&buf, data); err != nil {
				return "", err
			}
			defer buf.Reset()
			return buf.String(), nil
		}

		args := make([]string, 0, len(service.Args))
		for _, arg := range service.Args {
			arg, err := interp(arg)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(arg) == "" {
				continue
			}
			args = append(args, arg)
		}
		var err error
		for k, v := range service.Env {
			service.Env[k], err = interp(v)
			if err != nil {
				return nil, err
			}
		}
		data.Env = service.Env

		if service.Image == "" {
			service.Image = "flynn/" + service.ID
		}

		job := &host.Job{
			ID:       cluster.RandomJobID("flynn-" + service.ID + "-"),
			TCPPorts: 1,
			Config: &docker.Config{
				Image:        service.Image,
				Entrypoint:   service.Entrypoint,
				Cmd:          args,
				AttachStdout: true,
				AttachStderr: true,
				Env:          dockerEnv(data.Env),
				Volumes:      data.Volumes,
				ExposedPorts: make(map[string]struct{}, len(service.TCPPorts)),
			},
			HostConfig: &docker.HostConfig{
				PortBindings: make(map[string][]docker.PortBinding, len(service.TCPPorts)),
			},
		}
		job.Config.Env = append(job.Config.Env, "EXTERNAL_IP="+m.externalAddr)

		for i, port := range service.TCPPorts {
			job.TCPPorts = 0
			if i == 0 {
				job.Config.Env = append(job.Config.Env, "PORT="+port)
			}
			job.Config.Env = append(job.Config.Env, fmt.Sprintf("PORT_%d=%s", i, port))
			job.Config.ExposedPorts[port+"/tcp"] = struct{}{}
			job.HostConfig.PortBindings[port+"/tcp"] = []docker.PortBinding{{HostPort: port, HostIp: m.bindAddr}}
		}

		if err := m.backend.Run(job); err != nil {
			return nil, err
		}

		container, err := m.backend.(*DockerBackend).docker.InspectContainer(job.ID)
		if err != nil {
			return nil, err
		}

		data.InternalIP = container.NetworkSettings.IPAddress
		data.readonly = true
		serviceData[service.ID] = data
	}

	return serviceData, nil
}
