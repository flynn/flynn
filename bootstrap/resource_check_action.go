package bootstrap

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/httphelper"
)

type ResourceCheckAction struct {
	Ports []host.Port
}

func init() {
	Register("resource-check", &ResourceCheckAction{})
}

func (a *ResourceCheckAction) Run(s *State) error {
	conflicts := make(map[*cluster.Host][]host.Port)
	for _, h := range s.Hosts {
		req := host.ResourceCheck{Ports: a.Ports}
		if err := h.ResourceCheck(req); err != nil {
			if j, ok := err.(httphelper.JSONError); ok {
				var resp host.ResourceCheck
				if err := json.Unmarshal(j.Detail, &resp); err != nil {
					return err
				}
				conflicts[h] = resp.Ports
			} else {
				return err
			}
		}
	}
	if len(conflicts) > 0 {
		conflictMsg := "conflicts detected!\n\nThe following hosts have conflicting services listening on ports Flynn is configured to use:\n"
		for host, ports := range conflicts {
			hostIP, _, err := net.SplitHostPort(host.Addr())
			if err != nil {
				return err
			}
			conflictMsg += hostIP + ": "
			for _, port := range ports {
				conflictMsg += fmt.Sprintf("%s:%d ", port.Proto, port.Port)
			}
		}
		conflictMsg += "\n\nAfter you correct the above errors re-run bootstrap to continue setting up Flynn."
		return fmt.Errorf(conflictMsg)
	}
	return nil
}
