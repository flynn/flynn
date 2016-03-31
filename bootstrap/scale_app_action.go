package bootstrap

import (
	ct "github.com/flynn/flynn/controller/types"
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
	if a.AppStep != "" {
		data, err := getAppStep(s, a.AppStep)
		if err != nil {
			return err
		}
		a.Formation.AppID = data.App.ID
		a.Formation.ReleaseID = data.Release.ID
	}

	for name, count := range a.Formation.Processes {
		if s.Singleton && count > 1 {
			a.Formation.Processes[name] = 1
		}
	}

	return client.PutFormation(a.Formation)
}
