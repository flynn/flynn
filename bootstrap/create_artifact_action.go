package bootstrap

import (
	ct "github.com/flynn/flynn/controller/types"
)

type CreateArtifactAction struct {
	ID string `json:"id"`

	Artifact *ct.Artifact `json:"artifact"`
}

func init() {
	Register("create-artifact", &CreateArtifactAction{})
}

func (a *CreateArtifactAction) Run(s *State) error {
	client, err := s.ControllerClient()
	if err != nil {
		return err
	}
	if err := client.CreateArtifact(a.Artifact); err != nil {
		return err
	}
	s.StepData[a.ID] = a.Artifact
	return nil
}
