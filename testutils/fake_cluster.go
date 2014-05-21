package testutils

import (
	"errors"

	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-flynn/cluster"
)

func NewFakeCluster() *FakeCluster {
	return &FakeCluster{hostClients: make(map[string]cluster.Host)}
}

type FakeCluster struct {
	hosts       map[string]host.Host
	hostClients map[string]cluster.Host
}

func (c *FakeCluster) ListHosts() (map[string]host.Host, error) {
	return c.hosts, nil
}

func (c *FakeCluster) GetHost(id string) host.Host {
	return c.hosts[id]
}

func (c *FakeCluster) DialHost(id string) (cluster.Host, error) {
	client, ok := c.hostClients[id]
	if !ok {
		return nil, errors.New("FakeCluster: unknown host")
	}
	return client, nil
}

func (c *FakeCluster) AddJobs(req *host.AddJobsReq) (*host.AddJobsRes, error) {
	for hostID, jobs := range req.HostJobs {
		host, ok := c.hosts[hostID]
		if !ok {
			return nil, errors.New("FakeCluster: unknown host")
		}
		host.Jobs = append(host.Jobs, jobs...)
		c.hosts[hostID] = host
	}
	return &host.AddJobsRes{State: c.hosts}, nil
}

func (c *FakeCluster) SetHosts(h map[string]host.Host) {
	c.hosts = h
}

func (c *FakeCluster) SetHostClient(id string, h cluster.Host) {
	c.hostClients[id] = h
}
