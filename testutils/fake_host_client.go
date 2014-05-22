package testutils

import (
	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-flynn/cluster"
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
	cluster *FakeCluster
}

func (c *FakeHostClient) ListJobs() (map[string]host.ActiveJob, error)                 { return nil, nil }
func (c *FakeHostClient) GetJob(id string) (*host.ActiveJob, error)                    { return nil, nil }
func (c *FakeHostClient) StreamEvents(id string, ch chan<- *host.Event) cluster.Stream { return nil }
func (c *FakeHostClient) Close() error                                                 { return nil }
func (c *FakeHostClient) Attach(req *host.AttachReq, wait bool) (cluster.ReadWriteCloser, func() error, error) {
	f, ok := c.attach[req.JobID]
	if !ok {
		f = c.attach["*"]
	}
	return f(req, wait)
}

func (c *FakeHostClient) StopJob(id string) error {
	c.stopped[id] = true
	c.cluster.RemoveJob(c.hostID, id)
	return nil
}

func (c *FakeHostClient) IsStopped(id string) bool {
	return c.stopped[id]
}

func (c *FakeHostClient) SetAttach(id string, rwc cluster.ReadWriteCloser) {
	c.attach[id] = func(*host.AttachReq, bool) (cluster.ReadWriteCloser, func() error, error) {
		return rwc, nil, nil
	}
}

func (c *FakeHostClient) SetAttachFunc(id string, f attachFunc) {
	c.attach[id] = f
}

type attachFunc func(req *host.AttachReq, wait bool) (cluster.ReadWriteCloser, func() error, error)
