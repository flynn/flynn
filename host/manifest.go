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

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/technoweenie/grohl"
	"github.com/flynn/flynn/host/ports"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
)

func parseEnviron() map[string]string {
	env := os.Environ()
	res := make(map[string]string, len(env))
	for _, v := range env {
		kv := strings.SplitN(v, "=", 2)
		res[kv[0]] = kv[1]
	}

	if _, ok := res["ETCD_NAME"]; !ok {
		res["ETCD_NAME"] = random.String(8)
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
		return 0, fmt.Errorf("host: invalid TCPPort(%d), ManifestData is read-only", id)
	}
	if id < len(m.TCPPorts) {
		return m.TCPPorts[id], nil
	} else if id > len(m.TCPPorts) {
		return 0, fmt.Errorf("host: invalid TCPPort(%d), expecting id <= %d", id, len(m.TCPPorts))
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
	state        *State
	ports        map[string]*ports.Allocator
}

type manifestService struct {
	ID         string            `json:"id"`
	Image      string            `json:"image"`
	ImageID    string            `json:"image_id"`
	Entrypoint []string          `json:"entrypoint"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env"`
	ExposeEnv  []string          `json:"expose_env"`
	TCPPorts   []string          `json:"tcp_ports"`
}

func (m *manifestRunner) runManifest(r io.Reader) (map[string]*ManifestData, error) {
	g := grohl.NewContext(grohl.Data{"fn": "run_manifest"})
	var services []*manifestService
	if err := json.NewDecoder(r).Decode(&services); err != nil {
		return nil, err
	}

	serviceData := make(map[string]*ManifestData, len(services))

	m.state.mtx.Lock()
	for _, job := range m.state.jobs {
		if job.ManifestID == "" || job.Status != host.StatusRunning {
			continue
		}
		var service *manifestService
		for _, service = range services {
			if service.ID == job.ManifestID {
				break
			}
		}
		if service == nil {
			continue
		}
		g.Log(grohl.Data{"at": "restore", "service": service.ID, "job.id": job.Job.ID})

		data := &ManifestData{
			ExternalIP: m.externalAddr,
			InternalIP: job.InternalIP,
			Env:        job.Job.Config.Env,
			Services:   serviceData,
			ports:      m.ports["tcp"],
			readonly:   true,
		}
		data.TCPPorts = make([]int, 0, len(job.Job.Config.Ports))
		for _, p := range job.Job.Config.Ports {
			if p.Proto != "tcp" {
				continue
			}
			data.TCPPorts = append(data.TCPPorts, p.Port)
		}
		serviceData[service.ID] = data
	}
	m.state.mtx.Unlock()

	for _, service := range services {
		if _, exists := serviceData[service.ID]; exists {
			continue
		}

		data := &ManifestData{
			Env:        parseEnviron(),
			Services:   serviceData,
			ExternalIP: m.externalAddr,
			ports:      m.ports["tcp"],
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
			service.Image = "https://registry.hub.docker.com/flynn/" + service.ID
		}
		if service.ImageID != "" {
			service.Image += "?id=" + service.ImageID
		}

		job := &host.Job{
			ID: cluster.RandomJobID("flynn-" + service.ID + "-"),
			Artifact: host.Artifact{
				Type: "docker",
				URI:  service.Image,
			},
			Config: host.ContainerConfig{
				Entrypoint:  service.Entrypoint,
				Cmd:         args,
				Env:         data.Env,
				HostNetwork: true,
			},
		}
		if job.Config.Env == nil {
			job.Config.Env = make(map[string]string)
		}
		job.Config.Env["EXTERNAL_IP"] = m.externalAddr

		for _, k := range service.ExposeEnv {
			if v := os.Getenv(k); v != "" {
				job.Config.Env[k] = v
			}
		}

		job.Config.Ports = make([]host.Port, len(data.TCPPorts))
		for i, port := range data.TCPPorts {
			job.Config.Ports[i] = host.Port{Proto: "tcp", Port: port}
		}
		if len(job.Config.Ports) == 0 {
			job.Config.Ports = []host.Port{{Proto: "tcp"}}
		}

		if err := m.backend.Run(job); err != nil {
			return nil, err
		}

		m.state.SetManifestID(job.ID, service.ID)
		activeJob := m.state.GetJob(job.ID)
		data.InternalIP = activeJob.InternalIP
		data.readonly = true
		serviceData[service.ID] = data

		if service.ID == "flannel" {
			if err := m.backend.ConfigureNetworking(NetworkStrategyFlannel, job.ID); err != nil {
				return nil, err
			}
		}
	}

	return serviceData, nil
}
