package bootstrap

import (
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/sirenia/client"
)

type SireniaWaitAction struct {
	Service string `json:"service"`
}

func init() {
	Register("sirenia-wait", &SireniaWaitAction{})
}

func (a *SireniaWaitAction) Run(s *State) error {
	// use discoverd client to lookup leader
	d, err := s.DiscoverdClient()
	if err != nil {
		return err
	}

	var leader *discoverd.Instance
	err = attempt.Strategy{
		Min:   5,
		Total: 5 * time.Minute,
		Delay: 500 * time.Millisecond,
	}.Run(func() error {
		leader, err = d.Service(a.Service).Leader()
		return err
	})
	if err != nil {
		return err
	}

	// connect using sirenia client and wait until database reports read/write
	return client.NewClient(leader.Addr).WaitForReadWrite(5 * time.Minute)
}
