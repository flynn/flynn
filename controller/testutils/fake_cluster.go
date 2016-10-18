package testutils

import (
	"errors"
	"fmt"
	"sync"

	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/stream"
)

func NewFakeCluster() *FakeCluster {
	return NewFakeClusterWithHosts(nil)
}

func NewFakeClusterWithHosts(hosts map[string]utils.HostClient) *FakeCluster {
	if hosts == nil {
		hosts = make(map[string]utils.HostClient)
	}
	return &FakeCluster{
		hosts:        hosts,
		hostChannels: make(map[chan *discoverd.Event]struct{}),
	}
}

type FakeCluster struct {
	hosts        map[string]utils.HostClient
	mtx          sync.RWMutex
	hostChannels map[chan *discoverd.Event]struct{}
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

func (c *FakeCluster) StreamHostEvents(ch chan *discoverd.Event) (stream.Stream, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if _, ok := c.hostChannels[ch]; ok {
		return nil, errors.New("Already streaming that channel")
	}
	c.hostChannels[ch] = struct{}{}

	for id := range c.hosts {
		ch <- createDiscoverdEvent(id, discoverd.EventKindUp)
	}
	ch <- createDiscoverdEvent("", discoverd.EventKindCurrent)

	return &ClusterStream{cluster: c, ch: ch}, nil
}

func (c *FakeCluster) Host(id string) (utils.HostClient, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	host, ok := c.hosts[id]
	if !ok {
		return nil, fmt.Errorf("Host with id %q not found", id)
	}
	return host, nil
}

func (c *FakeCluster) SetHosts(h map[string]utils.HostClient) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	oldHosts := c.hosts
	c.hosts = make(map[string]utils.HostClient)
	for id, h := range h {
		if _, ok := oldHosts[id]; ok {
			delete(oldHosts, id)
			c.hosts[id] = h
		} else {
			c.addHost(h)
		}
	}
	for id := range oldHosts {
		c.removeHost(id)
	}
}

func (c *FakeCluster) AddHost(h utils.HostClient) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.addHost(h)
}

func (c *FakeCluster) addHost(h utils.HostClient) {
	c.hosts[h.ID()] = h

	for ch := range c.hostChannels {
		ch <- createDiscoverdEvent(h.ID(), discoverd.EventKindUp)
	}
}

func (c *FakeCluster) RemoveHost(hostID string) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.removeHost(hostID)
}

func (c *FakeCluster) removeHost(hostID string) {
	delete(c.hosts, hostID)

	for ch := range c.hostChannels {
		ch <- createDiscoverdEvent(hostID, discoverd.EventKindDown)
	}
}

func createDiscoverdEvent(hostID string, k discoverd.EventKind) *discoverd.Event {
	return &discoverd.Event{
		Kind: k,
		Instance: &discoverd.Instance{
			Meta: map[string]string{
				"id": hostID,
			},
		},
	}
}

type ClusterStream struct {
	cluster *FakeCluster
	ch      chan *discoverd.Event
}

func (c *ClusterStream) Close() error {
	c.cluster.mtx.Lock()
	defer c.cluster.mtx.Unlock()
	delete(c.cluster.hostChannels, c.ch)
	close(c.ch)
	return nil
}

func (c *ClusterStream) Err() error {
	return nil
}
