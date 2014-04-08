package bootstrap

import (
	"fmt"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/strowger/types"
)

type AddRouteAction struct {
	ID string `json:"id"`

	AppStep string `json:"app_step"`
	*strowger.Route
}

func init() {
	Register("add-route", &AddRouteAction{})
}

type AddRouteState struct {
	App   *ct.App
	Route *strowger.Route
}

func (a *AddRouteAction) Run(s *State) error {
	client, err := s.ControllerClient()
	if err != nil {
		return err
	}
	data, err := getAppStep(s, a.AppStep)
	if err != nil {
		return err
	}

	if err := client.CreateRoute(data.App.ID, a.Route); err != nil {
		return err
	}
	s.StepData[a.ID] = &AddRouteState{App: data.App, Route: a.Route}

	return nil
}

func getAppStep(s *State, step string) (*AppState, error) {
	data, ok := s.StepData[step].(*AppState)
	if !ok {
		return nil, fmt.Errorf("bootstrap: unable to find step %q", step)
	}
	return data, nil
}

func (a *AddRouteAction) Cleanup(s *State) error { return nil }
