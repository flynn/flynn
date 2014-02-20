package cluster

import (
	"errors"
	"io"
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
		if closer, ok := c.c.(io.Closer); ok {
			closer.Close()
		}
		c.err = Attempts.Run(func() (err error) {
			c.c, err = rpcplus.DialHTTP("tcp", update.Addr)
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

func (c *Client) ConnectHost(id string) (Host, error) {
	// TODO: reuse connection if leader id == id
	services := c.service.Select(map[string]string{"id": id})
	if len(services) == 0 {
		return nil, ErrNoServers
	}
	rc, err := rpcplus.DialHTTP("tcp", services[0].Addr)
	return &hostClient{service: c.service, c: rc}, err
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
