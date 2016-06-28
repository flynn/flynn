package bootstrap

import (
	"fmt"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/random"
)

type AddAppAction struct {
	ID string `json:"id"`

	FromStep string  `json:"from_step"`
	App      *ct.App `json:"app"`
}

func init() {
	Register("add-app", &AddAppAction{})
}

type AppState struct {
	*ct.ExpandedFormation
	Formation *ct.Formation  `json:"formation"`
	Resources []*ct.Resource `json:"resources"`
}

func (a *AddAppAction) Run(s *State) error {
	data, ok := s.StepData[a.FromStep].(*RunAppState)
	if !ok {
		return fmt.Errorf("bootstrap: unable to find step %q", a.FromStep)
	}
	as := &AppState{
		ExpandedFormation: &ct.ExpandedFormation{},
		Resources:         make([]*ct.Resource, 0, len(data.Resources)),
	}
	s.StepData[a.ID] = as

	client, err := s.ControllerClient()
	if err != nil {
		return err
	}

	a.App.ID = data.App.ID
	if err := client.CreateApp(a.App); err != nil {
		return err
	}
	as.App = a.App
	for _, artifact := range data.Artifacts {
		if err := client.CreateArtifact(artifact); err != nil {
			return err
		}
	}
	as.Artifacts = data.Artifacts
	if err := client.CreateRelease(a.App.ID, data.Release); err != nil {
		return err
	}
	as.Release = data.Release

	for i, p := range data.Providers {
		if provider, ok := s.Providers[p.Name]; ok {
			p = provider
		} else {
			if err := client.CreateProvider(p); err != nil {
				return err
			}
			s.Providers[p.Name] = p
		}

		resource := &ct.Resource{
			ID:         random.UUID(),
			Apps:       []string{a.App.ID},
			ProviderID: p.ID,
			ExternalID: data.Resources[i].ID,
			Env:        data.Resources[i].Env,
		}
		if err := client.PutResource(resource); err != nil {
			return err
		}
		as.Resources = append(as.Resources, resource)
	}

	formation := &ct.Formation{
		AppID:     data.App.ID,
		ReleaseID: data.Release.ID,
		Processes: data.Processes,
	}
	if err := client.PutFormation(formation); err != nil {
		return err
	}
	as.Formation = formation
	if err := client.SetAppRelease(data.App.ID, data.Release.ID); err != nil {
		return err
	}

	return nil
}
