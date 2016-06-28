package bootstrap

import (
	"time"

	ct "github.com/flynn/flynn/controller/types"
)

type DeployAppAction struct {
	ID string `json:"id"`

	*ct.ExpandedFormation
	App       *ct.App        `json:"app"`
	Resources []*ct.Provider `json:"resources"`
}

func init() {
	Register("deploy-app", &DeployAppAction{})
}

func interpolateRelease(s *State, r *ct.Release) {
	for k, v := range r.Env {
		r.Env[k] = interpolate(s, v)
		if r.Env[k] == "" {
			delete(r.Env, k)
		}
	}
	for _, proc := range r.Processes {
		for k, v := range proc.Env {
			proc.Env[k] = interpolate(s, v)
			if proc.Env[k] == "" {
				delete(proc.Env, k)
			}
		}
	}
}

func (a *DeployAppAction) Run(s *State) error {
	as := &AppState{
		ExpandedFormation: &ct.ExpandedFormation{},
		Resources:         make([]*ct.Resource, 0, len(a.Resources)),
	}
	s.StepData[a.ID] = as

	client, err := s.ControllerClient()
	if err != nil {
		return err
	}

	if err := client.CreateApp(a.App); err != nil {
		return err
	}
	as.App = a.App

	if a.Release.Env == nil {
		a.Release.Env = make(map[string]string)
	}
	interpolateRelease(s, a.Release)
	for _, p := range a.Resources {
		if provider, ok := s.Providers[p.Name]; ok {
			p = provider
		} else {
			if err := client.CreateProvider(p); err != nil {
				return err
			}
			s.Providers[p.Name] = p
		}

		res, err := client.ProvisionResource(&ct.ResourceReq{ProviderID: p.ID, Apps: []string{a.App.ID}})
		if err != nil {
			return err
		}
		as.Resources = append(as.Resources, res)

		for k, v := range res.Env {
			a.Release.Env[k] = v
		}
	}

	a.Release.ArtifactIDs = make([]string, len(a.Artifacts))
	for i, artifact := range a.Artifacts {
		if err := client.CreateArtifact(artifact); err != nil {
			return err
		}
		a.Release.ArtifactIDs[i] = artifact.ID
	}
	as.Artifacts = a.Artifacts

	if err := client.CreateRelease(a.App.ID, a.Release); err != nil {
		return err
	}
	as.Release = a.Release

	formation := &ct.Formation{
		AppID:     a.App.ID,
		ReleaseID: a.Release.ID,
		Processes: a.Processes,
	}
	for name, count := range formation.Processes {
		if s.Singleton && count > 1 {
			formation.Processes[name] = 1
		}
	}
	if err := client.PutFormation(formation); err != nil {
		return err
	}
	as.Formation = formation

	timeoutCh := make(chan struct{})
	time.AfterFunc(5*time.Minute, func() { close(timeoutCh) })
	return client.DeployAppRelease(a.App.ID, a.Release.ID, timeoutCh)
}
