package testutils

import (
	"fmt"
	"time"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
)

func NewFakeHostClient(hostID string) *FakeHostClient {
	return &FakeHostClient{
		hostID:  hostID,
		stopped: make(map[string]bool),
		attach:  make(map[string]attachFunc),
		volumes: make(map[string]*volume.Info),
		Jobs:    make(map[string]host.ActiveJob),
	}
}

type FakeHostClient struct {
	hostID  string
	stopped map[string]bool
	attach  map[string]attachFunc
	Jobs    map[string]host.ActiveJob
	cluster *FakeCluster
	volumes map[string]*volume.Info
}

func (c *FakeHostClient) ID() string { return c.hostID }

func (c *FakeHostClient) Attach(req *host.AttachReq, wait bool) (cluster.AttachClient, error) {
	f, ok := c.attach[req.JobID]
	if !ok {
		f = c.attach["*"]
	}
	return f(req, wait)
}

func (c *FakeHostClient) ListJobs() (map[string]host.ActiveJob, error) {
	return c.Jobs, nil
}

func (c *FakeHostClient) AddJob(job *host.Job) error {
	c.Jobs[job.ID] = host.ActiveJob{Job: job, HostID: c.hostID, StartedAt: time.Now()}
	return nil
}

func (c *FakeHostClient) GetJob(id string) (*host.ActiveJob, error) {
	job, ok := c.Jobs[id]
	if !ok {
		return nil, fmt.Errorf("unable to find job with ID %q", id)
	}
	return &job, nil
}

func (c *FakeHostClient) StopJob(id string) error {
	c.stopped[id] = true
	delete(c.Jobs, id)
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

func (c *FakeHostClient) CreateVolume(providerID string) (*volume.Info, error) {
	id := random.UUID()
	volume := &volume.Info{ID: id}
	c.volumes[id] = volume
	return volume, nil
}

type attachFunc func(req *host.AttachReq, wait bool) (cluster.AttachClient, error)
