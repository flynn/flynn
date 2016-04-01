package bootstrap

import (
	"fmt"
	"time"

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
		for h := range hosts {
			status, err := h.GetStatus()
			if err != nil {
				continue
			}
			if status.Network != nil && status.Network.Subnet != "" && status.Discoverd != nil && status.Discoverd.URL != "" {
				delete(hosts, h)
				up++
			}
		}
		if up >= s.MinHosts {
			break outer
		}

		select {
		case <-timeout:
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
