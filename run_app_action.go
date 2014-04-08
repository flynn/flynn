package bootstrap

import (
	"errors"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-controller/utils"
	"github.com/flynn/go-flynn/resource"
)

type RunAppAction struct {
	*ct.ExpandedFormation

	ID        string         `json:"id"`
	AppStep   string         `json:"app_step"`
	Resources []*ct.Provider `json:"resources,omitempty"`
	HostTags  []string       `json:"host_tags,omitempty"`
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
	Providers []*ct.Provider
	Resources []*resource.Resource
	Jobs      []Job
}

type Job struct {
	HostID string
	JobID  string
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

	if a.App == nil || a.App.ID == "" {
		a.App = &ct.App{ID: utils.UUID()}
	}
	if a.Artifact == nil {
		return errors.New("bootstrap: artifact must be set")
	}
	if a.Artifact.ID == "" {
		a.Artifact.ID = utils.UUID()
	}
	if a.Release == nil {
		return errors.New("bootstrap: release must be set")
	}
	if a.Release.ID == "" {
		a.Release.ID = utils.UUID()
	}
	a.Release.ArtifactID = a.Artifact.ID
	if a.Release.Env == nil {
		a.Release.Env = make(map[string]string)
	}

	for _, p := range a.Resources {
		server, err := resource.NewServer(p.URL)
		if err != nil {
			return err
		}
		res, err := server.Provision(nil)
		server.Close()
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
		for i := 0; i < count; i++ {
			config, err := utils.JobConfig(a.ExpandedFormation, typ)
			if err != nil {
				return err
			}
			job, err := startJob(s, a.HostTags, config)
			if err != nil {
				return err
			}
			as.Jobs = append(as.Jobs, *job)
		}
	}

	return nil
}

func (a *RunAppAction) Cleanup(s *State) error {
	data, ok := s.StepData[a.ID].(*RunAppState)
	if !ok {
		return nil
	}

	// TODO: delete provisioned resources

	if len(data.Jobs) == 0 {
		return nil
	}

	cc, err := s.ClusterClient()
	if err != nil {
		return err
	}
	for _, job := range data.Jobs {
		h, err := cc.ConnectHost(job.HostID)
		if err != nil {
			continue
		}
		defer h.Close()
		if err := h.StopJob(job.JobID); err != nil {
			continue
		}
	}
	return nil
}
