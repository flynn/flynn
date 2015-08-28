package testutils

import (
	"errors"
	"fmt"
	"sync"

	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/pkg/stream"
)

func NewFakeCluster() *FakeCluster {
	return &FakeCluster{
		hosts:        make(map[string]*FakeHostClient),
		hostChannels: make(map[chan utils.HostClient]struct{}),
	}
}

type FakeCluster struct {
	hosts        map[string]*FakeHostClient
	mtx          sync.RWMutex
	hostChannels map[chan utils.HostClient]struct{}
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

func (c *FakeCluster) StreamHosts(ch chan utils.HostClient) (stream.Stream, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if _, ok := c.hostChannels[ch]; ok {
		return nil, errors.New("Already streaming that channel")
	}
	c.hostChannels[ch] = struct{}{}

	for _, h := range c.hosts {
		ch <- h
	}

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

func (c *FakeCluster) SetHosts(h map[string]*FakeHostClient) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.hosts = h
}

func (c *FakeCluster) AddHost(h *FakeHostClient) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	h.cluster = c
	c.hosts[h.ID()] = h

	for ch := range c.hostChannels {
		ch <- h
	}
}

type ClusterStream struct {
	cluster *FakeCluster
	ch      chan utils.HostClient
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
