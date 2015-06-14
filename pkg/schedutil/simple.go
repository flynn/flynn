package schedutil

import (
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
)

type HostSlice []*cluster.Host

func PickHost(hosts HostSlice) *cluster.Host {
	if len(hosts) == 0 {
		return nil
	}
	// Return a random pick
	return hosts[random.Math.Intn(len(hosts))]
}
