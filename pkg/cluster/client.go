// Package cluster implements a client for the Flynn host service.
package cluster

import (
	"errors"
	"sync"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/rpcplus"
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

// NewClient uses discoverd to dial the local cluster leader and returns
// a client.
func NewClient() (*Client, error) {
	return NewClientWithDial(nil, nil)
}

// A ServiceSetFunc is a function that takes a service name and returns
// a discoverd.ServiceSet.
type ServiceSetFunc func(name string) (discoverd.ServiceSet, error)

// NewClientWithDial uses the provided dial and services to dial the cluster
// leader and returns a client. If dial is nil, the default network dialer is
// used. If services is nil, the default discoverd client is used.
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

// A LocalClient implements Client methods against an in-process leader.
type LocalClient interface {
	ListHosts() (map[string]host.Host, error)
	AddJobs(*host.AddJobsReq) (*host.AddJobsRes, error)
	RegisterHost(*host.Host, chan *host.Job) Stream
	RemoveJobs([]string) error
}

// NewClientWithSelf returns a client configured to use self to talk to the
// leader with the identifier id.
func NewClientWithSelf(id string, self LocalClient) (*Client, error) {
	client, err := newClient(nil)
	if err != nil {
		return nil, err
	}
	client.selfID = id
	client.self = self
	return client, client.start()
}

// A Client is used to interact with the leader of a Flynn host service cluster
// leader. If the leader changes, the client uses service discovery to connect
// to the new leader automatically.
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

// Close disconnects from the server and cleans up internal resources used by
// the client.
func (c *Client) Close() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.c != nil {
		c.c.Close()
	}
	return c.service.Close()
}

// LeaderID returns the identifier of the current leader.
func (c *Client) LeaderID() string {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.leaderID
}

// ListHosts returns a map of host ids to host structures containing metadata
// and job lists.
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

// AddJobs requests the addition of more jobs to the cluster.
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

// DialHost dials and returns a host client for the specified host identifier.
func (c *Client) DialHost(id string) (Host, error) {
	// TODO: reuse connection if leader id == id
	services := c.service.Select(map[string]string{"id": id})
	if len(services) == 0 {
		return nil, ErrNoServers
	}
	addr := services[0].Addr
	rc, err := rpcplus.DialHTTPPath("tcp", addr, rpcplus.DefaultRPCPath, c.dial)
	return NewHostClient(addr, rc, c.dial), err
}

// RegisterHost is used by the host service to register itself with the leader
// and get a stream of new jobs. It is not used by clients.
func (c *Client) RegisterHost(host *host.Host, jobs chan *host.Job) Stream {
	if c := c.local(); c != nil {
		return c.RegisterHost(host, jobs)
	}
	client, err := c.RPCClient()
	if err != nil {
		return rpcStream{&rpcplus.Call{Error: err}}
	}
	return rpcStream{client.StreamGo("Cluster.RegisterHost", host, jobs)}
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

// StreamHostEvents sends a stream of host events from the host to ch.
func (c *Client) StreamHostEvents(ch chan<- *host.HostEvent) Stream {
	return rpcStream{c.c.StreamGo("Cluster.StreamHostEvents", struct{}{}, ch)}
}

// RPCClient returns the underlying client used to communicate with the server.
func (c *Client) RPCClient() (RPCClient, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.c, c.err
}

// An RPCClient implements the methods used by Client to communicate with
// servers. The typical concrete implementation is a *rpcplus.Client.
type RPCClient interface {
	Call(serviceMethod string, args interface{}, reply interface{}) error
	Go(serviceMethod string, args interface{}, reply interface{}, done chan *rpcplus.Call) *rpcplus.Call
	StreamGo(serviceMethod string, args interface{}, replyStream interface{}) *rpcplus.Call
	Close() error
}

// RandomJobID returns a random job identifier with an optional prefix.
func RandomJobID(prefix string) string { return prefix + random.UUID() }
