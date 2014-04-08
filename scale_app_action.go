package bootstrap

import (
	ct "github.com/flynn/flynn-controller/types"
)

type ScaleAppAction struct {
	AppStep string `json:"app_step"`

	*ct.Formation
}

func init() {
	Register("scale-app", &ScaleAppAction{})
}

func (a *ScaleAppAction) Run(s *State) error {
	client, err := s.ControllerClient()
	if err != nil {
		return err
	}
	data, err := getAppStep(s, a.AppStep)
	if err != nil {
		return err
	}

	a.Formation.AppID = data.App.ID
	a.Formation.ReleaseID = data.Release.ID

	return client.PutFormation(a.Formation)
}

func (a *ScaleAppAction) Cleanup(s *State) error { return nil }
