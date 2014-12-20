package schedutil

import (
	"sort"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/random"
)

type HostSlice []host.Host

func (p HostSlice) Len() int           { return len(p) }
func (p HostSlice) Less(i, j int) bool { return len(p[i].Jobs) < len(p[j].Jobs) }
func (p HostSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func PickHost(hosts HostSlice) *host.Host {
	if len(hosts) == 0 {
		return nil
	}

	// Sort hosts in ascending order of number of active jobs
	sort.Sort(hosts)

	// Get all hosts that are tied for the lowest number of jobs
	low := len(hosts[0].Jobs)
	var highIdx int
	for i, h := range hosts {
		if len(h.Jobs) == low {
			highIdx = i
		}
	}
	if highIdx == 0 {
		return &hosts[0]
	}

	// Return a random pick from the hosts with the least jobs
	return &hosts[random.Math.Intn(highIdx+1)]
}
