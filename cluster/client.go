package cluster

import (
	"errors"
	"sync"

	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/rpcplus"
)

var ErrNoServers = errors.New("cluster: no servers found")

func NewClient() (*Client, error) {
	services, err := discoverd.NewServiceSet("flynn-host")
	if err != nil {
		return nil, err
	}
	client := &Client{service: services}
	firstErr := make(chan error)
	go client.followLeader(firstErr)
	return client, <-firstErr
}

func (c *Client) followLeader(firstErr chan<- error) {
	for update := range c.service.Leaders() {
		c.mtx.Lock()
		if closer, ok := c.c.(interface {
			Close() error
		}); ok {
			closer.Close()
		}
		c.c, c.err = rpcplus.DialHTTP("tcp", update.Addr)
		// TODO: use attempt package to retry here
		c.mtx.Unlock()
		if firstErr != nil {
			firstErr <- c.err
			firstErr = nil
		}
	}
	// TODO: reconnect to discoverd here
}

type Client struct {
	service discoverd.ServiceSet

	c   RPCClient
	mtx sync.RWMutex
	err error
}

func (c *Client) ListHosts() (map[string]host.Host, error) {
	c.mtx.RLock()
	if err := c.err; err != nil {
		c.mtx.RUnlock()
		return nil, err
	}
	client := c.c
	c.mtx.RUnlock()

	var state map[string]host.Host
	err := client.Call("Cluster.ListHosts", struct{}{}, &state)
	return state, err
}

func (c *Client) AddJobs(req *host.AddJobsReq) (*host.AddJobsRes, error) {
	c.mtx.RLock()
	if err := c.err; err != nil {
		c.mtx.RUnlock()
		return nil, err
	}
	client := c.c
	c.mtx.RUnlock()

	var res host.AddJobsRes
	err := client.Call("Cluster.AddJobs", req, &res)
	return &res, err
}

func (c *Client) ConnectHost(id string) (*Host, error) {
	// TODO: reuse connection if leader id == id
	services := c.service.Select(map[string]string{"id": id})
	if len(services) == 0 {
		return nil, ErrNoServers
	}
	rc, err := rpcplus.DialHTTP("tcp", services[0].Addr)
	return &Host{service: c.service, c: rc}, err
}

func (c *Client) RPCClient() (RPCClient, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.c, c.err
}

type Host struct {
	service discoverd.ServiceSet

	c RPCClient
}

type RPCClient interface {
	Call(serviceMethod string, args interface{}, reply interface{}) error
	Go(serviceMethod string, args interface{}, reply interface{}, done chan *rpcplus.Call) *rpcplus.Call
	StreamGo(serviceMethod string, args interface{}, replyStream interface{}) *rpcplus.Call
}

func (c *Host) ListJobs() (map[string]host.ActiveJob, error) {
	var jobs map[string]host.ActiveJob
	err := c.c.Call("Host.ListJobs", struct{}{}, &jobs)
	return jobs, err
}

func (c *Host) GetJob(id string) (*host.ActiveJob, error) {
	var res host.ActiveJob
	err := c.c.Call("Host.GetJob", id, &res)
	return &res, err
}

func (c *Host) StopJob(id string) error {
	return c.c.Call("Host.StopJob", id, &struct{}{})
}
