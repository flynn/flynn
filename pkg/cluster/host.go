package cluster

import (
	"fmt"
	"net/http"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/httpclient"
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
	StreamEvents(id string, ch chan<- *host.Event) (stream.Stream, error)

	// Attach attaches to a job, optionally waiting for it to start before
	// attaching.
	Attach(req *host.AttachReq, wait bool) (AttachClient, error)

	// Creates a new volume, returning its ID.
	// When in doubt, use a providerId of "default".
	CreateVolume(providerId string) (*volume.Info, error)
}

type hostClient struct {
	c *httpclient.Client
}

// NewHostClient creates a new Host that uses client to communicate with it.
// addr is used by Attach.
func NewHostClient(addr string, h *http.Client) Host {
	if h == nil {
		h = http.DefaultClient
	}
	return &hostClient{c: &httpclient.Client{
		ErrPrefix:   "host",
		ErrNotFound: ErrNotFound,
		URL:         addr,
		HTTP:        h,
	}}
}

func (c *hostClient) ListJobs() (map[string]host.ActiveJob, error) {
	var jobs map[string]host.ActiveJob
	err := c.c.Get("/host/jobs", &jobs)
	return jobs, err
}

func (c *hostClient) GetJob(id string) (*host.ActiveJob, error) {
	var res host.ActiveJob
	err := c.c.Get(fmt.Sprintf("/host/jobs/%s", id), &res)
	return &res, err
}

func (c *hostClient) StopJob(id string) error {
	return c.c.Delete(fmt.Sprintf("/host/jobs/%s", id))
}

func (c *hostClient) StreamEvents(id string, ch chan<- *host.Event) (stream.Stream, error) {
	r := fmt.Sprintf("/host/jobs/%s", id)
	if id == "all" {
		r = "/host/jobs"
	}
	return c.c.Stream("GET", r, nil, ch)
}

func (c *hostClient) CreateVolume(providerId string) (*volume.Info, error) {
	var res volume.Info
	err := c.c.Post(fmt.Sprintf("/volume/provider/%s/newVolume", providerId), nil, &res)
	return &res, err
}
