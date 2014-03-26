package bootstrap

import (
	"fmt"

	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-flynn/cluster"
	"github.com/flynn/go-flynn/resource"
)

type RunJobAction struct {
	ID        string     `json:"id"`
	Job       *host.Job  `json:"job"`
	HostTags  []string   `json:"host_tags"`
	Resources []Resource `json:"resources"`
}

type Resource struct {
	Service string `json:"service"`
	Path    string `json:"path"`
}

func init() {
	Register("run-job", &RunJobAction{})
}

type RunJobState struct {
	Resources []*resource.Resource
	HostID    string
	JobID     string
}

func (a *RunJobAction) Run(s *State) error {
	js := &RunJobState{
		Resources: make([]*resource.Resource, len(a.Resources)),
	}
	s.StepData[a.ID] = js

	for i, r := range a.Resources {
		server, err := resource.NewServer(r.Service, r.Path)
		if err != nil {
			return err
		}
		res, err := server.Provision()
		server.Close()
		if err != nil {
			return err
		}
		js.Resources[i] = res
		for k, v := range res.Env {
			a.Job.Config.Env = append(a.Job.Config.Env, k+"="+v)
		}
	}

	cc, err := s.ClusterClient()
	if err != nil {
		return err
	}
	h, err := randomHost(cc)
	if err != nil {
		return err
	}

	// TODO: filter by tags

	a.Job.ID = cluster.RandomJobID("")
	js.HostID = h.ID
	js.JobID = a.Job.ID

	hc, err := cc.ConnectHost(h.ID)
	if err != nil {
		return err
	}
	defer hc.Close()

	jobStatus := make(chan error)
	go func() {
		events := make(chan *host.Event)
		stream := hc.StreamEvents(js.JobID, events)
		defer stream.Close()
		for e := range events {
			switch e.Event {
			case "start", "stop":
				jobStatus <- nil
				return
			case "error":
				job, err := hc.GetJob(js.JobID)
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
		}
		jobStatus <- fmt.Errorf("bootstrap: host job stream disconnected unexpectedly: %q", stream.Err())
	}()

	_, err = cc.AddJobs(&host.AddJobsReq{HostJobs: map[string][]*host.Job{h.ID: {a.Job}}})
	if err != nil {
		return err
	}

	return <-jobStatus
}

func randomHost(cc *cluster.Client) (*host.Host, error) {
	hosts, err := cc.ListHosts()
	if err != nil {
		return nil, err
	}

	for _, host := range hosts {
		return &host, nil
	}
	return nil, cluster.ErrNoServers
}

func (a *RunJobAction) Cleanup(s *State) error {
	data, ok := s.StepData[a.ID].(*RunJobState)
	if !ok {
		return nil
	}

	if data.HostID != "" && data.JobID != "" {
		cc, err := s.ClusterClient()
		if err != nil {
			return err
		}
		h, err := cc.ConnectHost(data.HostID)
		if err != nil {
			return err
		}
		defer h.Close()
		if err := h.StopJob(data.JobID); err != nil {
			return err
		}
	}

	// TODO: delete provisioned resources the API exists

	return nil
}
