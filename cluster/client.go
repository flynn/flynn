package cluster

import (
	"errors"
	"sync"
	"time"

	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-flynn/attempt"
	"github.com/flynn/rpcplus"
)

// ErrNoServers is returned if no host servers are found
var ErrNoServers = errors.New("cluster: no servers found")

// Attempts is the attempt strategy that is used to connect to the leader.
// It must not be modified after the first call to NewClient.
var Attempts = attempt.Strategy{
	Min:   5,
	Total: 5 * time.Second,
	Delay: 200 * time.Millisecond,
}

func NewClient() (*Client, error) {
	return NewClientWithDial(nil, nil)
}

func NewClientWithDial(dial rpcplus.DialFunc, services func(name string) (discoverd.ServiceSet, error)) (*Client, error) {
	if services == nil {
		services = discoverd.NewServiceSet
	}
	ss, err := services("flynn-host")
	if err != nil {
		return nil, err
	}
	client := &Client{dial: dial, service: ss}
	firstErr := make(chan error)
	go client.followLeader(firstErr)
	return client, <-firstErr
}

func (c *Client) followLeader(firstErr chan<- error) {
	for update := range c.service.Leaders() {
		if update == nil {
			if firstErr != nil {
				firstErr <- ErrNoServers
				firstErr = nil
			}
			continue
		}
		c.mtx.Lock()
		if c.c != nil {
			c.c.Close()
		}
		c.leaderID = update.Attrs["id"]
		c.err = Attempts.Run(func() (err error) {
			c.c, err = rpcplus.DialHTTPPath("tcp", update.Addr, rpcplus.DefaultRPCPath, c.dial)
			return
		})
		c.mtx.Unlock()
		if firstErr != nil {
			firstErr <- c.err
			firstErr = nil
		}
	}
	// TODO: reconnect to discoverd here
}

type Client struct {
	service  discoverd.ServiceSet
	leaderID string

	dial rpcplus.DialFunc
	c    RPCClient
	mtx  sync.RWMutex
	err  error
}

func (c *Client) Close() error {
	c.service.Close()
	return c.c.Close()
}

func (c *Client) LeaderID() string {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.leaderID
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

func (c *Client) ConnectHost(id string) (Host, error) {
	// TODO: reuse connection if leader id == id
	services := c.service.Select(map[string]string{"id": id})
	if len(services) == 0 {
		return nil, ErrNoServers
	}
	rc, err := rpcplus.DialHTTPPath("tcp", services[0].Addr, rpcplus.DefaultRPCPath, c.dial)
	return newHostClient(c.service, rc, c.dial), err
}

func (c *Client) RPCClient() (RPCClient, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.c, c.err
}

type RPCClient interface {
	Call(serviceMethod string, args interface{}, reply interface{}) error
	Go(serviceMethod string, args interface{}, reply interface{}, done chan *rpcplus.Call) *rpcplus.Call
	StreamGo(serviceMethod string, args interface{}, replyStream interface{}) *rpcplus.Call
	Close() error
}
