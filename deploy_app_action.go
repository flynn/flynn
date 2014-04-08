package bootstrap

import ct "github.com/flynn/flynn-controller/types"

type DeployAppAction struct {
	ID string `json:"id"`

	*ct.ExpandedFormation
	App       *ct.App        `json:"app"`
	Resources []*ct.Provider `json:"resources"`
}

func init() {
	Register("deploy-app", &DeployAppAction{})
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
	for _, p := range a.Resources {
		if provider, ok := s.Providers[p.Name]; ok {
			p = provider
		} else {
			if err := client.CreateProvider(p); err != nil {
				return err
			}
			s.Providers[p.Name] = p
		}

		res, err := client.ProvisionResource(&ct.ResourceReq{ProviderID: p.ID})
		if err != nil {
			return err
		}
		as.Resources = append(as.Resources, res)

		for k, v := range res.Env {
			a.Release.Env[k] = v
		}
	}

	if err := client.CreateArtifact(a.Artifact); err != nil {
		return err
	}
	as.Artifact = a.Artifact

	a.Release.ArtifactID = a.Artifact.ID
	if err := client.CreateRelease(a.Release); err != nil {
		return err
	}
	as.Release = a.Release

	formation := &ct.Formation{
		AppID:     a.App.ID,
		ReleaseID: a.Release.ID,
		Processes: a.Processes,
	}
	if err := client.PutFormation(formation); err != nil {
		return err
	}
	as.Formation = formation

	if err := client.SetAppRelease(a.App.ID, a.Release.ID); err != nil {
		return err
	}

	return nil
}

func (a *DeployAppAction) Cleanup(s *State) error {
	// TODO
	return nil
}
