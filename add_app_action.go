package bootstrap

import (
	"fmt"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-controller/utils"
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
	Formation *ct.Formation
	Resources []*ct.Resource
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
	if err := client.CreateArtifact(data.Artifact); err != nil {
		return err
	}
	as.Artifact = data.Artifact
	if err := client.CreateRelease(data.Release); err != nil {
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
			ID:         utils.UUID(),
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

func (a *AddAppAction) Cleanup(s *State) error {
	// TODO
	return nil
}
