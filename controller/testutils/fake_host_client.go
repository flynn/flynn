package testutils

import (
	"errors"
	"fmt"
	"sync"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/stream"
)

func NewFakeHostClient(hostID string, sync bool) *FakeHostClient {
	h := &FakeHostClient{
		hostID:        hostID,
		stopped:       make(map[string]bool),
		attach:        make(map[string]attachFunc),
		volumes:       make(map[string]*volume.Info),
		Jobs:          make(map[string]host.ActiveJob),
		eventChannels: make(map[chan<- *host.Event]struct{}),
		Healthy:       true,
	}
	if sync {
		h.TestEventHook = make(chan struct{})
	}
	return h
}

type FakeHostClient struct {
	hostID           string
	stopped          map[string]bool
	attach           map[string]attachFunc
	Jobs             map[string]host.ActiveJob
	volumes          map[string]*volume.Info
	eventChannelsMtx sync.Mutex
	eventChannels    map[chan<- *host.Event]struct{}
	jobsMtx          sync.RWMutex
	Healthy          bool
	TestEventHook    chan struct{}
}

func (c *FakeHostClient) ID() string { return c.hostID }

func (c *FakeHostClient) Tags() map[string]string { return nil }

func (c *FakeHostClient) Attach(req *host.AttachReq, wait bool) (cluster.AttachClient, error) {
	f, ok := c.attach[req.JobID]
	if !ok {
		f = c.attach["*"]
	}
	return f(req, wait)
}

func (c *FakeHostClient) ListJobs() (map[string]host.ActiveJob, error) {
	c.jobsMtx.RLock()
	defer c.jobsMtx.RUnlock()
	jobs := make(map[string]host.ActiveJob)
	for id, j := range c.Jobs {
		jobs[id] = j
	}
	return jobs, nil
}

func (c *FakeHostClient) AddJob(job *host.Job) error {
	c.jobsMtx.Lock()
	defer c.jobsMtx.Unlock()
	if _, ok := c.Jobs[job.ID]; ok {
		return errors.New("job exists")
	}
	j := host.ActiveJob{
		Job:       job,
		HostID:    c.hostID,
		Status:    host.StatusStarting,
		StartedAt: time.Now(),
	}
	c.Jobs[job.ID] = j

	c.eventChannelsMtx.Lock()
	defer c.eventChannelsMtx.Unlock()
	for ch := range c.eventChannels {
		ch <- &host.Event{
			Event: host.JobEventStart,
			JobID: job.ID,
			Job:   &j,
		}
		if c.TestEventHook != nil {
			<-c.TestEventHook
		}
	}
	return nil
}

func (c *FakeHostClient) GetJob(id string) (*host.ActiveJob, error) {
	c.jobsMtx.RLock()
	defer c.jobsMtx.RUnlock()
	job, ok := c.Jobs[id]
	if !ok {
		return nil, fmt.Errorf("unable to find job with ID %q", id)
	}
	return &job, nil
}

func (c *FakeHostClient) StopJob(id string) error {
	c.jobsMtx.Lock()
	defer c.jobsMtx.Unlock()
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

func (c *FakeHostClient) DiscoverdDeregisterJob(id string) error {
	return nil
}

func (c *FakeHostClient) stop(id string) error {
	job := c.Jobs[id]
	delete(c.Jobs, id)
	c.eventChannelsMtx.Lock()
	defer c.eventChannelsMtx.Unlock()
	for ch := range c.eventChannels {
		ch <- &host.Event{
			Event: host.JobEventStop,
			JobID: id,
			Job:   &job,
		}
		if c.TestEventHook != nil {
			<-c.TestEventHook
		}
	}
	return nil
}

func (c *FakeHostClient) CrashJob(uuid string) error {
	c.jobsMtx.Lock()
	defer c.jobsMtx.Unlock()
	id := cluster.GenerateJobID(c.hostID, uuid)
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
	c.jobsMtx.RLock()
	defer c.jobsMtx.RUnlock()
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

func (c *FakeHostClient) StreamEvents(id string, ch chan *host.Event) (stream.Stream, error) {
	c.eventChannelsMtx.Lock()
	if _, ok := c.eventChannels[ch]; ok {
		c.eventChannelsMtx.Unlock()
		return nil, errors.New("Already streaming that channel")
	}
	c.eventChannels[ch] = struct{}{}
	c.eventChannelsMtx.Unlock()

	for _, j := range c.Jobs {
		ch <- &host.Event{
			Event: host.JobEventStart,
			JobID: j.Job.ID,
			Job:   &j,
		}
	}

	return &HostStream{host: c, ch: ch}, nil
}

func (c *FakeHostClient) GetStatus() (*host.HostStatus, error) {
	if !c.Healthy {
		return nil, errors.New("unhealthy")
	}
	return &host.HostStatus{ID: c.ID()}, nil
}

func (c *FakeHostClient) GetSinks() ([]*ct.Sink, error) {
	return nil, nil
}

func (c *FakeHostClient) AddSink(*ct.Sink) error {
	return nil
}

func (c *FakeHostClient) RemoveSink(string) error {
	return nil
}

type attachFunc func(req *host.AttachReq, wait bool) (cluster.AttachClient, error)

type HostStream struct {
	host *FakeHostClient
	ch   chan *host.Event
}

func (h *HostStream) Close() error {
	go func() {
		for range h.ch {
		}
	}()
	h.host.eventChannelsMtx.Lock()
	delete(h.host.eventChannels, h.ch)
	h.host.eventChannelsMtx.Unlock()
	close(h.ch)
	return nil
}

func (h *HostStream) Err() error {
	return nil
}
