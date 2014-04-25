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

type ServiceSetFunc func(name string) (discoverd.ServiceSet, error)

func NewClientWithDial(dial rpcplus.DialFunc, services ServiceSetFunc) (*Client, error) {
	client, err := newClient(services)
	if err != nil {
		return nil, err
	}
	client.dial = dial
	return client, client.start()
}

func newClient(services ServiceSetFunc) (*Client, error) {
	if services == nil {
		services = discoverd.NewServiceSet
	}
	ss, err := services("flynn-host")
	if err != nil {
		return nil, err
	}
	return &Client{service: ss, leaderChange: make(chan struct{})}, nil
}

type LocalClient interface {
	ListHosts() (map[string]host.Host, error)
	AddJobs(*host.AddJobsReq) (*host.AddJobsRes, error)
	RegisterHost(*host.Host, chan *host.Job) *error
	RemoveJobs([]string) error
}

func NewClientWithSelf(id string, self LocalClient) (*Client, error) {
	client, err := newClient(nil)
	if err != nil {
		return nil, err
	}
	client.selfID = id
	client.self = self
	return client, client.start()
}

type Client struct {
	service  discoverd.ServiceSet
	leaderID string

	dial rpcplus.DialFunc
	c    RPCClient
	mtx  sync.RWMutex
	err  error

	selfID string
	self   LocalClient

	leaderChange chan struct{}
}

func (c *Client) start() error {
	firstErr := make(chan error)
	go c.followLeader(firstErr)
	return <-firstErr
}

func (c *Client) followLeader(firstErr chan<- error) {
	for update := range c.service.Leaders() {
		if update == nil {
			if firstErr != nil {
				firstErr <- ErrNoServers
				c.Close()
				return
			}
			continue
		}
		c.mtx.Lock()
		if c.c != nil {
			c.c.Close()
			c.c = nil
		}
		c.leaderID = update.Attrs["id"]
		if c.leaderID != c.selfID {
			c.err = Attempts.Run(func() (err error) {
				c.c, err = rpcplus.DialHTTPPath("tcp", update.Addr, rpcplus.DefaultRPCPath, c.dial)
				return
			})
		}
		if c.err == nil {
			close(c.leaderChange)
			c.leaderChange = make(chan struct{})
		}
		c.mtx.Unlock()
		if firstErr != nil {
			firstErr <- c.err
			if c.err != nil {
				c.c = nil
				c.Close()
				return
			}
			firstErr = nil
		}
	}
	// TODO: reconnect to discoverd here
}

func (c *Client) local() LocalClient {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	if c.leaderID == c.selfID {
		return c.self
	}
	return nil
}

// NewLeaderSignal returns a channel that strobes exactly once when a new leader
// connection has been established successfully. It is an error to attempt to
// receive more than one value from the channel.
func (c *Client) NewLeaderSignal() <-chan struct{} {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.leaderChange
}

func (c *Client) Close() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.c != nil {
		c.c.Close()
	}
	return c.service.Close()
}

func (c *Client) LeaderID() string {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.leaderID
}

func (c *Client) ListHosts() (map[string]host.Host, error) {
	if c := c.local(); c != nil {
		return c.ListHosts()
	}
	client, err := c.RPCClient()
	if err != nil {
		return nil, err
	}
	var state map[string]host.Host
	return state, client.Call("Cluster.ListHosts", struct{}{}, &state)
}

func (c *Client) AddJobs(req *host.AddJobsReq) (*host.AddJobsRes, error) {
	if c := c.local(); c != nil {
		return c.AddJobs(req)
	}
	client, err := c.RPCClient()
	if err != nil {
		return nil, err
	}
	var res host.AddJobsRes
	return &res, client.Call("Cluster.AddJobs", req, &res)
}

func (c *Client) DialHost(id string) (Host, error) {
	// TODO: reuse connection if leader id == id
	services := c.service.Select(map[string]string{"id": id})
	if len(services) == 0 {
		return nil, ErrNoServers
	}
	rc, err := rpcplus.DialHTTPPath("tcp", services[0].Addr, rpcplus.DefaultRPCPath, c.dial)
	return newHostClient(c.service, rc, c.dial), err
}

// Register is used by flynn-host to register itself with the leader and get
// a stream of new jobs. It is not used by clients.
func (c *Client) RegisterHost(host *host.Host, jobs chan *host.Job) *error {
	if c := c.local(); c != nil {
		return c.RegisterHost(host, jobs)
	}
	client, err := c.RPCClient()
	if err != nil {
		return &err
	}
	return &client.StreamGo("Cluster.RegisterHost", host, jobs).Error
}

// RemoveJobs is used by flynn-host to delete jobs from the cluster state. It
// does not actually kill jobs running on hosts, and must not be used by
// clients.
func (c *Client) RemoveJobs(jobIDs []string) error {
	if c := c.local(); c != nil {
		return c.RemoveJobs(jobIDs)
	}
	client, err := c.RPCClient()
	if err != nil {
		return err
	}
	return client.Call("Cluster.RemoveJobs", jobIDs, &struct{}{})
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
