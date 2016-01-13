package bootstrap

import (
	"encoding/json"
	"fmt"

	"github.com/flynn/flynn/discoverd/client"
)

type MonitorMetadata struct {
	Enabled bool `json:"enabled,omitempty"`
	Hosts   int  `json:"hosts,omitempty"`
}

type ClusterMonitorAction struct {
	Enabled bool `json:"enabled"`
}

func init() {
	Register("cluster-monitor", &ClusterMonitorAction{})
}

func (c *ClusterMonitorAction) Run(s *State) error {
	data, err := json.Marshal(MonitorMetadata{
		Enabled: c.Enabled,
		Hosts:   len(s.Hosts),
	})
	if err != nil {
		fmt.Println("failed to encode cluster-monitor metadata")
		return err
	}
	err = discoverd.DefaultClient.AddService("cluster-monitor", nil)
	if err != nil {
		return err
	}
	return discoverd.NewService("cluster-monitor").SetMeta(&discoverd.ServiceMeta{
		Data:  data,
		Index: 0,
	})
}
