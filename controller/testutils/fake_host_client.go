package testutils

import (
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

func NewFakeHostClient(hostID string) *FakeHostClient {
	return &FakeHostClient{
		hostID:  hostID,
		stopped: make(map[string]bool),
		attach:  make(map[string]attachFunc),
	}
}

type FakeHostClient struct {
	hostID  string
	stopped map[string]bool
	attach  map[string]attachFunc
	Jobs    []*host.Job
	cluster *FakeCluster
}

func (c *FakeHostClient) ID() string { return c.hostID }

func (c *FakeHostClient) Attach(req *host.AttachReq, wait bool) (cluster.AttachClient, error) {
	f, ok := c.attach[req.JobID]
	if !ok {
		f = c.attach["*"]
	}
	return f(req, wait)
}

func (c *FakeHostClient) AddJob(job *host.Job) error {
	c.Jobs = append(c.Jobs, job)
	return nil
}

func (c *FakeHostClient) StopJob(id string) error {
	c.stopped[id] = true
	return nil
}

func (c *FakeHostClient) IsStopped(id string) bool {
	return c.stopped[id]
}

func (c *FakeHostClient) SetAttach(id string, ac cluster.AttachClient) {
	c.attach[id] = func(*host.AttachReq, bool) (cluster.AttachClient, error) {
		return ac, nil
	}
}

func (c *FakeHostClient) SetAttachFunc(id string, f attachFunc) {
	c.attach[id] = f
}

type attachFunc func(req *host.AttachReq, wait bool) (cluster.AttachClient, error)
