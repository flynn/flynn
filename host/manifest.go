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
	"github.com/flynn/flynn/host/backend"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume/manager"
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
	ExternalIP  string
	BridgeIP    string
	Nameservers string
	TCPPorts    []int
	Volumes     map[string]struct{}
	Env         map[string]string
	Services    map[string]*ManifestData

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

	port := 5000 + len(m.TCPPorts)
	m.TCPPorts = append(m.TCPPorts, port)
	return port, nil
}

func (m *ManifestData) Volume(mntPath string) string {
	if m.Volumes == nil {
		m.Volumes = make(map[string]struct{})
	}
	m.Volumes[mntPath] = struct{}{}
	return mntPath
}

type manifestRunner struct {
	env          map[string]string
	externalAddr string
	bindAddr     string
	backend      backend.Backend
	state        *backend.State
	vman         *volumemanager.Manager
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
	Data       bool              `json:"data"`
}

func (m *manifestRunner) runManifest(r io.Reader) (map[string]*ManifestData, error) {
	g := grohl.NewContext(grohl.Data{"fn": "run_manifest"})
	var services []*manifestService
	if err := json.NewDecoder(r).Decode(&services); err != nil {
		return nil, err
	}

	serviceData := make(map[string]*ManifestData, len(services))

	m.state.Lock()
	for _, job := range m.state.Jobs {
		if job.ManifestID == "" || job.Status != host.StatusRunning && job.Status != host.StatusStarting {
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
			Env:        job.Job.Config.Env,
			Services:   serviceData,
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
	m.state.Unlock()

	var netInfo backend.NetworkInfo

	runService := func(service *manifestService) error {
		if _, exists := serviceData[service.ID]; exists {
			return nil
		}

		data := &ManifestData{
			Env:         parseEnviron(),
			Services:    serviceData,
			ExternalIP:  m.externalAddr,
			BridgeIP:    netInfo.BridgeAddr,
			Nameservers: strings.Join(netInfo.Nameservers, ","),
		}

		if service.Data {
			data.Volume("/data")
		}

		// Add explicit tcp ports to data.TCPPorts
		for _, port := range service.TCPPorts {
			port, err := strconv.Atoi(port)
			if err != nil {
				return err
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
				return err
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
				return err
			}
		}
		data.Env = service.Env

		if service.Image == "" {
			service.Image = "https://registry.hub.docker.com/flynn/" + service.ID
		}
		if service.ImageID != "" {
			service.Image += "?id=" + service.ImageID
		}

		// prepare named volumes
		volumeBindings := make([]host.VolumeBinding, 0, len(data.Volumes))
		for mntPath := range data.Volumes {
			vol, err := m.vman.NewVolume()
			if err != nil {
				return err
			}
			volumeBindings = append(volumeBindings, host.VolumeBinding{
				Target:    mntPath,
				VolumeID:  vol.Info().ID,
				Writeable: true,
			})
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
				Volumes:     volumeBindings,
			},
			Resurrect: true,
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

		if err := m.backend.Run(job, &backend.RunConfig{ManifestID: service.ID}); err != nil {
			return err
		}

		data.readonly = true
		serviceData[service.ID] = data
		return nil
	}

	for _, service := range services {
		if err := runService(service); err != nil {
			return nil, err
		}
		if service.ID == "flannel" {
			var job *host.Job
			for _, j := range m.state.Jobs {
				if j.ManifestID != service.ID {
					continue
				}
				job = j.Job
				break
			}
			if job == nil {
				return nil, fmt.Errorf("Could not find the flannel container!")
			}
			ni, err := m.backend.ConfigureNetworking(backend.NetworkStrategyFlannel, job.ID)
			if err != nil {
				return nil, err
			}
			netInfo = *ni
		}
	}

	return serviceData, nil
}
