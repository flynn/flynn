package bootstrap

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	hostresource "github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/resource"
)

type RunAppAction struct {
	*ct.ExpandedFormation

	ID        string         `json:"id"`
	AppStep   string         `json:"app_step"`
	Resources []*ct.Provider `json:"resources,omitempty"`
}

type Provider struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func init() {
	Register("run-app", &RunAppAction{})
}

type RunAppState struct {
	*ct.ExpandedFormation
	Providers []*ct.Provider       `json:"providers"`
	Resources []*resource.Resource `json:"resources"`
}

func (a *RunAppAction) Run(s *State) error {
	if a.AppStep != "" {
		data, err := getAppStep(s, a.AppStep)
		if err != nil {
			return err
		}
		a.App = data.App
		procs := a.Processes
		a.ExpandedFormation = data.ExpandedFormation
		a.Processes = procs
	}
	as := &RunAppState{
		ExpandedFormation: a.ExpandedFormation,
		Resources:         make([]*resource.Resource, 0, len(a.Resources)),
		Providers:         make([]*ct.Provider, 0, len(a.Resources)),
	}
	s.StepData[a.ID] = as

	if a.App == nil {
		a.App = &ct.App{}
	}
	if a.App.ID == "" {
		a.App.ID = random.UUID()
	}
	if a.Artifact == nil {
		return errors.New("bootstrap: artifact must be set")
	}
	if a.Artifact.ID == "" {
		a.Artifact.ID = random.UUID()
	}
	if a.Release == nil {
		return errors.New("bootstrap: release must be set")
	}
	if a.Release.ID == "" {
		a.Release.ID = random.UUID()
	}
	a.Release.ArtifactID = a.Artifact.ID
	if a.Release.Env == nil {
		a.Release.Env = make(map[string]string)
	}
	interpolateRelease(s, a.Release)

	for _, p := range a.Resources {
		u, err := url.Parse(p.URL)
		if err != nil {
			return err
		}
		lookupDiscoverdURLHost(u, time.Second)
		res, err := resource.Provision(u.String(), nil)
		if err != nil {
			return err
		}
		as.Providers = append(as.Providers, p)
		as.Resources = append(as.Resources, res)
		for k, v := range res.Env {
			a.Release.Env[k] = v
		}
	}

	for typ, count := range a.Processes {
		if s.Singleton && count > 1 {
			a.Processes[typ] = 1
			count = 1
		}
		hosts := s.ShuffledHosts()
		if a.ExpandedFormation.Release.Processes[typ].Omni {
			count = len(hosts)
		}
		for i := 0; i < count; i++ {
			host := hosts[i%len(hosts)]
			config := utils.JobConfig(a.ExpandedFormation, typ, host.ID())

			if a.App.Name == "discoverd" && typ == "app" {
				config.Config.Ports[0].Port = 1110
				ips := s.SortedHostIPs()
				peers := make([]string, len(ips))
				for i, ip := range ips {
					peers[i] = net.JoinHostPort(ip, "1110")
				}
				config.Config.Env["DISCOVERD"] = "none"
				config.Config.Env["DISCOVERD_PEERS"] = strings.Join(peers, ",")
			}

			hostresource.SetDefaults(&config.Resources)
			if a.ExpandedFormation.Release.Processes[typ].Data {
				if err := utils.ProvisionVolume(host, config); err != nil {
					return err
				}
			}
			if err := startJob(s, host, config); err != nil {
				return err
			}
		}
	}

	return nil
}

func startJob(s *State, hc *cluster.Host, job *host.Job) error {
	jobStatus := make(chan error)
	events := make(chan *host.Event)
	stream, err := hc.StreamEvents(job.ID, events)
	if err != nil {
		return err
	}
	go func() {
		defer stream.Close()
		for {
			select {
			case e := <-events:
				switch e.Event {
				case "start", "stop":
					jobStatus <- nil
					return
				case "error":
					job, err := hc.GetJob(job.ID)
					if err != nil {
						jobStatus <- err
						return
					}
					if job.Error == nil {
						jobStatus <- fmt.Errorf("bootstrap: unknown error from host")
						return
					}
					jobStatus <- fmt.Errorf("bootstrap: host error while launching job: %q", *job.Error)
					return
				default:
				}
			case <-time.After(10 * time.Second):
				jobStatus <- errors.New("bootstrap: timed out waiting for job event")
				return
			}
		}
		jobStatus <- fmt.Errorf("bootstrap: host job stream disconnected unexpectedly: %q", stream.Err())
	}()

	if err := hc.AddJob(job); err != nil {
		return err
	}

	return <-jobStatus
}
