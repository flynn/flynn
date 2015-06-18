package testutils

import (
	"sync"

	"github.com/flynn/flynn/controller/utils"
)

func NewFakeCluster() *FakeCluster {
	return &FakeCluster{hosts: make(map[string]*FakeHostClient)}
}

type FakeCluster struct {
	hosts map[string]*FakeHostClient
	mtx   sync.RWMutex
}

func (c *FakeCluster) Hosts() ([]utils.HostClient, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	hosts := make([]utils.HostClient, 0, len(c.hosts))
	for _, h := range c.hosts {
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func (c *FakeCluster) Host(id string) (utils.HostClient, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.hosts[id], nil
}

func (c *FakeCluster) SetHosts(h map[string]*FakeHostClient) {
	c.hosts = h
}

func (c *FakeCluster) AddHost(h *FakeHostClient) {
	h.cluster = c
	c.hosts[h.ID()] = h
}
