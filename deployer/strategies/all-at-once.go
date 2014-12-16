package strategy

import (
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/deployer/types"
)

type AllAtOnce struct {
	client *controller.Client
}

var _ Performer = AllAtOnce{}

func (s AllAtOnce) Perform(d *deployer.Deployment, events chan<- deployer.DeploymentEvent) error {
	stream, err := s.client.StreamJobEvents(d.AppID, 0)
	if err != nil {
		return err
	}
	defer stream.Close()

	f, err := s.client.GetFormation(d.AppID, d.OldReleaseID)
	if err != nil {
		return err
	}

	if err := s.client.PutFormation(&ct.Formation{
		AppID:     d.AppID,
		ReleaseID: d.NewReleaseID,
		Processes: f.Processes,
	}); err != nil {
		return err
	}
	expect := make(jobEvents)
	for typ, n := range f.Processes {
		expect[typ] = map[string]int{"up": n}
	}
	if _, _, err := waitForJobEvents(stream.Events, expect); err != nil {
		return err
	}
	if err := s.client.DeleteFormation(d.AppID, d.OldReleaseID); err != nil {
		return err
	}
	expect = make(jobEvents)
	for typ, n := range f.Processes {
		expect[typ] = map[string]int{"down": n}
	}
	if _, _, err := waitForJobEvents(stream.Events, expect); err != nil {
		return err
	}
	return nil
}
