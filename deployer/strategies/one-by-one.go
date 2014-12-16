package strategy

import (
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/deployer/types"
)

type OneByOne struct {
	client *controller.Client
}

var _ Performer = OneByOne{}

func (s OneByOne) Perform(d *deployer.Deployment, events chan<- deployer.DeploymentEvent) error {
	stream, err := s.client.StreamJobEvents(d.AppID, 0)
	if err != nil {
		return err
	}
	defer stream.Close()

	f, err := s.client.GetFormation(d.AppID, d.OldReleaseID)
	if err != nil {
		return err
	}

	oldFormation := f.Processes
	newFormation := map[string]int{}

	for typ, num := range f.Processes {
		for i := 0; i < num; i++ {
			// start one process
			newFormation[typ]++
			if err := s.client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.NewReleaseID,
				Processes: newFormation,
			}); err != nil {
				return err
			}
			if _, _, err := waitForJobEvents(stream.Events, jobEvents{typ: {"up": 1}}); err != nil {
				return err
			}
			// stop one process
			oldFormation[typ]--
			if err := s.client.PutFormation(&ct.Formation{
				AppID:     d.AppID,
				ReleaseID: d.OldReleaseID,
				Processes: oldFormation,
			}); err != nil {
				return err
			}
			if _, _, err := waitForJobEvents(stream.Events, jobEvents{typ: {"down": 1}}); err != nil {
				return err
			}
		}
	}
	if err := s.client.DeleteFormation(d.AppID, d.OldReleaseID); err != nil {
		return err
	}
	return nil
}
