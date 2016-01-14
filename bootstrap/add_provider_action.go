package bootstrap

import (
	ct "github.com/flynn/flynn/controller/types"
)

func init() {
	Register("add-provider", &AddProviderAction{})
}

// AddProvider registers a provider on the controller.
type AddProviderAction struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (a *AddProviderAction) Run(s *State) error {
	client, err := s.ControllerClient()
	if err != nil {
		return err
	}

	// Register provider with controller.
	p := &ct.Provider{Name: a.Name, URL: a.URL}
	if err := client.CreateProvider(p); err != nil {
		return err
	}

	// Add provider to bootstrap state.
	s.Providers[p.Name] = p

	return nil
}
