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

func (c *FakeCluster) RemoveJob(hostID, jobID string) error {
	h, ok := c.hosts[hostID]
	if !ok {
		return errors.New("FakeCluster: unknown host")
	}
	jobs := make([]*host.Job, 0, len(h.Jobs))
	for _, job := range h.Jobs {
		if job.ID != jobID {
			jobs = append(jobs, job)
		}
	}
	h.Jobs = jobs
	c.hosts[hostID] = h
	return nil
}

func (c *FakeCluster) SetHosts(h map[string]host.Host) {
	c.hosts = h
}

func (c *FakeCluster) SetHostClient(id string, h cluster.Host) {
	h.(*FakeHostClient).cluster = c
	c.hostClients[id] = h
}
