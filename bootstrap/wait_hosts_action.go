package bootstrap

import (
	"fmt"
	"time"

	discoverd "github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/cluster"
)

type WaitHostsAction struct{}

func init() {
	Register("wait-hosts", &WaitHostsAction{})
}

func (a *WaitHostsAction) Run(s *State) error {
	const waitInterval = 500 * time.Millisecond

	hosts := make(map[*cluster.Host]struct{}, len(s.Hosts))
	for _, h := range s.Hosts {
		hosts[h] = struct{}{}
	}

	timeout := time.After(s.HostTimeout)
	up := 0
outer:
	for {
		var instances []*discoverd.Instance
		disc, err := s.DiscoverdClient()
		if err != nil {
			goto wait
		}
		instances, err = disc.Service("flynn-host").Instances()
		if err != nil {
			goto wait
		}

		for h := range hosts {
			status, err := h.GetStatus()
			if err != nil {
				continue
			}
			if status.Network != nil && status.Network.Subnet != "" && status.Discoverd != nil && status.Discoverd.URL != "" {
				for _, inst := range instances {
					if inst.Addr == h.Addr() {
						delete(hosts, h)
						up++
						break
					}
				}
			}
		}
		if up >= s.MinHosts {
			break outer
		}

	wait:
		select {
		case <-timeout:
			if err != nil {
				return err
			}
			msg := "bootstrap: timed out waiting for hosts to come up\n\nThe following hosts were unreachable:\n"
			for host := range hosts {
				msg += "\t" + host.Addr() + "\n"
			}
			msg += "\n"
			return fmt.Errorf(msg)
		case <-time.After(waitInterval):
		}
	}
	return nil
}
