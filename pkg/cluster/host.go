package cluster

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pinkerton/layer"
	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/stream"
)

// Host is a client for a host daemon.
type Host struct {
	id string
	c  *httpclient.Client
}

// NewHostClient creates a new Host that uses client to communicate with it.
// addr is used by Attach.
func NewHost(id string, addr string, h *http.Client) *Host {
	if h == nil {
		h = http.DefaultClient
	}
	if !strings.HasPrefix(addr, "http") {
		addr = "http://" + addr
	}
	return &Host{
		id: id,
		c: &httpclient.Client{
			ErrNotFound: ErrNotFound,
			URL:         addr,
			HTTP:        h,
		},
	}
}

// ID returns the ID of the host this client communicates with.
func (c *Host) ID() string {
	return c.id
}

// Addr returns the IP/port that the host API is listening on.
func (c *Host) Addr() string {
	u, err := url.Parse(c.c.URL)
	if err != nil {
		return ""
	}
	return u.Host
}

func (c *Host) GetStatus() (*host.HostStatus, error) {
	var res host.HostStatus
	err := c.c.Get("/host/status", &res)
	return &res, err
}

// ListJobs lists the jobs running on the host.
func (c *Host) ListJobs() (map[string]host.ActiveJob, error) {
	var jobs map[string]host.ActiveJob
	err := c.c.Get("/host/jobs", &jobs)
	return jobs, err
}

// AddJob runs a job on the host.
func (c *Host) AddJob(job *host.Job) error {
	return c.c.Put(fmt.Sprintf("/host/jobs/%s", job.ID), job, nil)
}

// GetJob retrieves job details by ID.
func (c *Host) GetJob(id string) (*host.ActiveJob, error) {
	var res host.ActiveJob
	err := c.c.Get(fmt.Sprintf("/host/jobs/%s", id), &res)
	return &res, err
}

// StopJob stops a running job.
func (c *Host) StopJob(id string) error {
	return c.c.Delete(fmt.Sprintf("/host/jobs/%s", id))
}

// SignalJob sends a signal to a running job.
func (c *Host) SignalJob(id string, sig int) error {
	return c.c.Put(fmt.Sprintf("/host/jobs/%s/signal/%d", id, sig), nil, nil)
}

// StreamEvents about job state changes to ch. id may be "all" or a single
// job ID.
func (c *Host) StreamEvents(id string, ch chan<- *host.Event) (stream.Stream, error) {
	r := fmt.Sprintf("/host/jobs/%s", id)
	if id == "all" {
		r = "/host/jobs"
	}
	return c.c.Stream("GET", r, nil, ch)
}

// CreateVolume a new volume, returning its ID.
// When in doubt, use a providerId of "default".
func (c *Host) CreateVolume(providerId string) (*volume.Info, error) {
	var res volume.Info
	err := c.c.Post(fmt.Sprintf("/storage/providers/%s/volumes", providerId), nil, &res)
	return &res, err
}

func (c *Host) DestroyVolume(volumeID string) error {
	return c.c.Delete(fmt.Sprintf("/storage/volumes/%s", volumeID))
}

func (c *Host) CreateSnapshot(volumeID string) (*volume.Info, error) {
	var res volume.Info
	err := c.c.Put(fmt.Sprintf("/storage/volumes/%s/snapshot", volumeID), nil, &res)
	return &res, err
}

// PullSnapshot requests the host pull a snapshot from another host onto one of
// its volumes. Returns the info for the new snapshot.
func (c *Host) PullSnapshot(receiveVolID string, sourceHostID string, sourceSnapID string) (*volume.Info, error) {
	var res volume.Info
	pull := volume.PullCoordinate{
		HostID:     sourceHostID,
		SnapshotID: sourceSnapID,
	}
	err := c.c.Post(fmt.Sprintf("/storage/volumes/%s/pull_snapshot", receiveVolID), pull, &res)
	return &res, err
}

// SendSnapshot requests transfer of volume snapshot data (this is used by other
// hosts in service of the PullSnapshot request).
func (c *Host) SendSnapshot(snapID string, assumeHaves []json.RawMessage) (io.ReadCloser, error) {
	header := http.Header{
		"Accept": []string{"application/vnd.zfs.snapshot-stream"},
	}
	res, err := c.c.RawReq("GET", fmt.Sprintf("/storage/volumes/%s/send", snapID), header, assumeHaves, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// PullImages pulls images from a TUF repository using the local TUF file in tufDB
func (c *Host) PullImages(repository, driver, root string, tufDB io.Reader, ch chan<- *layer.PullInfo) (stream.Stream, error) {
	header := http.Header{"Content-Type": {"application/octet-stream"}}
	path := fmt.Sprintf("/host/pull-images?repository=%s&driver=%s&root=%s", repository, driver, root)
	return c.c.StreamWithHeader("POST", path, header, tufDB, ch)
}
