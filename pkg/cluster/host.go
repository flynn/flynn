package cluster

import (
	"net"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/rpcplus"
	"github.com/flynn/flynn/pkg/stream"
)

// Host is a client for a host daemon.
type Host interface {
	// ListJobs lists the jobs running on the host.
	ListJobs() (map[string]host.ActiveJob, error)

	// GetJob retrieves job details by ID.
	GetJob(id string) (*host.ActiveJob, error)

	// StopJob stops a running job.
	StopJob(id string) error

	// StreamEvents about job state changes to ch. id may be "all" or a single
	// job ID.
	StreamEvents(id string, ch chan<- *host.Event) stream.Stream

	// Attach attaches to a job, optionally waiting for it to start before
	// attaching.
	Attach(req *host.AttachReq, wait bool) (AttachClient, error)

	// Close frees the underlying connection to the host.
	Close() error
}

type hostClient struct {
	addr string
	dial rpcplus.DialFunc
	c    RPCClient
}

// NewHostClient creates a new Host that uses client to communicate with it.
// addr and dial are used by Attach. dial may be nil to use the default dialer.
func NewHostClient(addr string, client RPCClient, dial rpcplus.DialFunc) Host {
	c := &hostClient{addr: addr, dial: dial, c: client}
	if dial == nil {
		c.dial = net.Dial
	}
	return c
}

func (c *hostClient) ListJobs() (map[string]host.ActiveJob, error) {
	var jobs map[string]host.ActiveJob
	err := c.c.Call("Host.ListJobs", struct{}{}, &jobs)
	return jobs, err
}

func (c *hostClient) GetJob(id string) (*host.ActiveJob, error) {
	var res host.ActiveJob
	err := c.c.Call("Host.GetJob", id, &res)
	return &res, err
}

func (c *hostClient) StopJob(id string) error {
	return c.c.Call("Host.StopJob", id, &struct{}{})
}

func (c *hostClient) StreamEvents(id string, ch chan<- *host.Event) stream.Stream {
	return rpcStream{c.c.StreamGo("Host.StreamEvents", id, ch)}
}

func (c *hostClient) Close() error {
	return c.c.Close()
}

type rpcStream struct {
	call *rpcplus.Call
}

func (s rpcStream) Close() error {
	return s.call.CloseStream()
}

func (s rpcStream) Err() error {
	return s.call.Error
}
