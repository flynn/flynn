package cluster

import (
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-discoverd"
)

type Host interface {
	ListJobs() (map[string]host.ActiveJob, error)
	GetJob(id string) (*host.ActiveJob, error)
	StopJob(id string) error
	StreamEvents(id string, ch chan<- host.Event) *error
	Attach(req *host.AttachReq, wait bool) (ReadWriteCloser, func() error, error)
	Close() error
}

type hostClient struct {
	service discoverd.ServiceSet

	c RPCClient
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

func (c *hostClient) StreamEvents(id string, ch chan<- host.Event) *error {
	return &c.c.StreamGo("Host.StreamEvents", id, ch).Error
}

func (c *hostClient) Close() error {
	return c.c.Close()
}
