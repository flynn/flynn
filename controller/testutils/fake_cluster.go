package testutils

import (
	"errors"
	"sync"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/stream"
)

func NewFakeCluster() *FakeCluster {
	return &FakeCluster{hostClients: make(map[string]*FakeHostClient)}
}

type FakeCluster struct {
	hosts       map[string]host.Host
	hostClients map[string]*FakeHostClient
	mtx         sync.RWMutex
	listeners   []chan<- *host.HostEvent
	listenMtx   sync.RWMutex
}

func (c *FakeCluster) ListHosts() ([]host.Host, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	hosts := make([]host.Host, 0, len(c.hosts))
	for id := range c.hosts {
		hosts = append(hosts, c.GetHost(id))
	}
	return hosts, nil
}

func (c *FakeCluster) GetHost(id string) host.Host {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	h := c.hosts[id]

	// copy the jobs to avoid races
	jobs := make([]*host.Job, len(h.Jobs))
	copy(jobs, h.Jobs)

	return host.Host{ID: h.ID, Jobs: jobs, Metadata: h.Metadata}
}

func (c *FakeCluster) DialHost(id string) (cluster.Host, error) {
	client, ok := c.hostClients[id]
	if !ok {
		return nil, errors.New("FakeCluster: unknown host")
	}
	return client, nil
}

func (c *FakeCluster) AddJobs(req map[string][]*host.Job) (map[string]host.Host, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for hostID, jobs := range req {
		host, ok := c.hosts[hostID]
		if !ok {
			return nil, errors.New("FakeCluster: unknown host")
		}
		if client, ok := c.hostClients[hostID]; ok {
			for _, job := range jobs {
				client.SendEvent("start", job.ID)
			}
		}
		host.Jobs = append(host.Jobs, jobs...)
		c.hosts[hostID] = host
	}
	return c.hosts, nil
}

func (c *FakeCluster) RemoveJob(hostID, jobID string, errored bool) error {
	c.mtx.Lock()
	h, ok := c.hosts[hostID]
	if !ok {
		c.mtx.Unlock()
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
	c.mtx.Unlock()

	if client, ok := c.hostClients[hostID]; ok {
		if errored {
			client.SendEvent("error", jobID)
		} else {
			client.SendEvent("stop", jobID)
		}
	}
	return nil
}

func (c *FakeCluster) SetHosts(h map[string]host.Host) {
	c.hosts = h
}

func (c *FakeCluster) AddHost(h host.Host) {
	c.hosts[h.ID] = h
}

func (c *FakeCluster) SetHostClient(id string, h *FakeHostClient) {
	h.cluster = c
	c.hostClients[id] = h
}

func (c *FakeCluster) StreamHostEvents(ch chan<- *host.HostEvent) (stream.Stream, error) {
	c.listenMtx.Lock()
	defer c.listenMtx.Unlock()
	c.listeners = append(c.listeners, ch)
	return &FakeClusterHostEventStream{ch: ch}, nil
}

func (c *FakeCluster) SendEvent(hostID, event string) {
	c.listenMtx.RLock()
	defer c.listenMtx.RUnlock()
	e := &host.HostEvent{HostID: hostID, Event: event}
	for _, ch := range c.listeners {
		ch <- e
	}
}

type FakeClusterHostEventStream struct {
	ch chan<- *host.HostEvent
}

func (h *FakeClusterHostEventStream) Close() error {
	close(h.ch)
	return nil
}

func (h *FakeClusterHostEventStream) Err() error {
	return nil
}
