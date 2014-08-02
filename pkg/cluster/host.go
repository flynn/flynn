package cluster

import (
	"net"

	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/rpcplus"
)

type Host interface {
	ListJobs() (map[string]host.ActiveJob, error)
	GetJob(id string) (*host.ActiveJob, error)
	StopJob(id string) error
	StreamEvents(id string, ch chan<- *host.Event) Stream
	Attach(req *host.AttachReq, wait bool) (ReadWriteCloser, func() error, error)
	Close() error
}

type hostClient struct {
	service discoverd.ServiceSet

	dial rpcplus.DialFunc
	c    RPCClient
}

func newHostClient(ss discoverd.ServiceSet, client RPCClient, dial rpcplus.DialFunc) Host {
	c := &hostClient{service: ss, dial: dial, c: client}
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

func (c *hostClient) StreamEvents(id string, ch chan<- *host.Event) Stream {
	return rpcStream{c.c.StreamGo("Host.StreamEvents", id, ch)}
}

func (c *hostClient) Close() error {
	return c.c.Close()
}

type Stream interface {
	Close() error
	Err() error
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
