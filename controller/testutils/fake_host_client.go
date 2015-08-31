package testutils

import (
	"errors"
	"fmt"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/stream"
)

func NewFakeHostClient(hostID string) *FakeHostClient {
	return &FakeHostClient{
		hostID:        hostID,
		stopped:       make(map[string]bool),
		attach:        make(map[string]attachFunc),
		volumes:       make(map[string]*volume.Info),
		Jobs:          make(map[string]host.ActiveJob),
		eventChannels: make(map[chan<- *host.Event]struct{}),
	}
}

type FakeHostClient struct {
	hostID        string
	stopped       map[string]bool
	attach        map[string]attachFunc
	Jobs          map[string]host.ActiveJob
	cluster       *FakeCluster
	volumes       map[string]*volume.Info
	eventChannels map[chan<- *host.Event]struct{}
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
	j := host.ActiveJob{Job: job, HostID: c.hostID, StartedAt: time.Now()}
	c.Jobs[job.ID] = j

	for ch := range c.eventChannels {
		ch <- &host.Event{
			Event: host.JobEventStart,
			JobID: job.ID,
			Job:   &j,
		}
	}
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
	job, ok := c.Jobs[id]
	if ok {
		switch job.Status {
		case host.StatusStarting:
			job.Status = host.StatusFailed
		case host.StatusRunning:
			job.Status = host.StatusDone
		default:
			return nil
		}
		c.Jobs[id] = job
		return c.stop(id)
	} else {
		return ct.NotFoundError{Resource: id}
	}
}

func (c *FakeHostClient) stop(id string) error {
	job := c.Jobs[id]
	delete(c.Jobs, id)
	for ch := range c.eventChannels {
		ch <- &host.Event{
			Event: host.JobEventStop,
			JobID: id,
			Job:   &job,
		}
	}
	return nil
}

func (c *FakeHostClient) CrashJob(id string) error {
	c.stopped[id] = true
	job, ok := c.Jobs[id]
	if ok {
		job.Status = host.StatusCrashed
		c.Jobs[id] = job
		return c.stop(id)
	} else {
		return ct.NotFoundError{Resource: id}
	}
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

func (c *FakeHostClient) StreamEvents(id string, ch chan<- *host.Event) (stream.Stream, error) {
	if _, ok := c.eventChannels[ch]; ok {
		return nil, errors.New("Already streaming that channel")
	}
	c.eventChannels[ch] = struct{}{}

	for _, j := range c.Jobs {
		ch <- &host.Event{
			Event: host.JobEventStart,
			JobID: j.Job.ID,
			Job:   &j,
		}
	}

	return &HostStream{host: c, ch: ch}, nil
}

type attachFunc func(req *host.AttachReq, wait bool) (cluster.AttachClient, error)

type HostStream struct {
	host *FakeHostClient
	ch   chan<- *host.Event
}

func (h *HostStream) Close() error {
	delete(h.host.eventChannels, h.ch)
	close(h.ch)
	return nil
}

func (h *HostStream) Err() error {
	return nil
}
