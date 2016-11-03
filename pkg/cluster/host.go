package cluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/stream"
)

// Host is a client for a host daemon.
type Host struct {
	id   string
	tags map[string]string
	c    *httpclient.Client
}

// NewHost creates a new Host that uses client to communicate with it.
// addr is used by Attach.
func NewHost(id string, addr string, h *http.Client, tags map[string]string) *Host {
	if h == nil {
		h = http.DefaultClient
	}
	if !strings.HasPrefix(addr, "http") {
		addr = "http://" + addr
	}
	return &Host{
		id:   id,
		tags: tags,
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

// Tags returns the hosts tags
func (c *Host) Tags() map[string]string {
	return c.tags
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

func WaitForHostStatus(hostIP string, desired func(*host.HostStatus) bool) (*host.HostStatus, error) {
	const waitInterval = 500 * time.Millisecond
	const logInterval = time.Minute
	start := time.Now()
	lastLogged := time.Now()
	h := NewHost("", fmt.Sprintf("http://%s:1113", hostIP), nil, nil)
	for {
		status, err := h.GetStatus()
		if err == nil && desired(status) {
			return status, nil
		}
		if time.Since(lastLogged) > logInterval {
			log.Printf("desired host status still not reached after %s", time.Since(start))
			lastLogged = time.Now()
		}
		time.Sleep(waitInterval)
	}
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

// DiscoverdDeregisterJob requests a job to deregister from service discovery.
func (c *Host) DiscoverdDeregisterJob(id string) error {
	return c.c.Put(fmt.Sprintf("/host/jobs/%s/discoverd-deregister", id), nil, nil)
}

// StreamEvents about job state changes to ch. id may be "all" or a single
// job ID.
func (c *Host) StreamEvents(id string, ch chan *host.Event) (stream.Stream, error) {
	r := fmt.Sprintf("/host/jobs/%s", id)
	if id == "all" {
		r = "/host/jobs"
	}
	return c.c.ResumingStream("GET", r, ch)
}

// CreateVolume a new volume, returning its ID.
// When in doubt, use a providerId of "default".
func (c *Host) CreateVolume(providerId string) (*volume.Info, error) {
	var res volume.Info
	err := c.c.Post(fmt.Sprintf("/storage/providers/%s/volumes", providerId), nil, &res)
	return &res, err
}

// GetVolume gets a volume by ID
func (c *Host) GetVolume(volumeID string) (*volume.Info, error) {
	var volume volume.Info
	return &volume, c.c.Get(fmt.Sprintf("/storage/volumes/%s", volumeID), &volume)
}

// ListVolume returns a list of volume IDs
func (c *Host) ListVolumes() ([]*volume.Info, error) {
	var volumes []*volume.Info
	return volumes, c.c.Get("/storage/volumes", &volumes)
}

// DestroyVolume deletes a volume by ID
func (c *Host) DestroyVolume(volumeID string) error {
	return c.c.Delete(fmt.Sprintf("/storage/volumes/%s", volumeID))
}

// Create snapshot creates a snapshot of a volume on a host.
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
func (c *Host) PullImages(repository, configDir, version string, tufDB io.Reader, ch chan *ct.ImagePullInfo) (stream.Stream, error) {
	header := http.Header{"Content-Type": {"application/octet-stream"}}
	query := make(url.Values)
	query.Set("repository", repository)
	query.Set("config-dir", configDir)
	query.Set("version", version)
	path := "/host/pull/images?" + query.Encode()
	return c.c.StreamWithHeader("POST", path, header, tufDB, ch)
}

// PullBinariesAndConfig pulls binaries and config from a TUF repository using the local TUF file in tufDB
func (c *Host) PullBinariesAndConfig(repository, binDir, configDir, version string, tufDB io.Reader) (map[string]string, error) {
	query := make(url.Values)
	query.Set("repository", repository)
	query.Set("bin-dir", binDir)
	query.Set("config-dir", configDir)
	query.Set("version", version)
	path := "/host/pull/binaries?" + query.Encode()
	var paths map[string]string
	return paths, c.c.Post(path, tufDB, &paths)
}

func (c *Host) ResourceCheck(request host.ResourceCheck) error {
	return c.c.Post("/host/resource-check", request, nil)
}

func (c *Host) Update(name string, args ...string) (pid int, err error) {
	cmd := &host.Command{Path: name, Args: args}
	return cmd.PID, c.c.Post("/host/update", cmd, cmd)
}

func (c *Host) UpdateTags(tags map[string]string) error {
	return c.c.Post("/host/tags", tags, nil)
}

func (c *Host) GetSinks() ([]*ct.Sink, error) {
	var sinks []*ct.Sink
	return sinks, c.c.Get("/sinks", &sinks)
}

func (c *Host) AddSink(info *ct.Sink) error {
	if info.ID == "" {
		return errors.New("missing ID")
	}
	return c.c.Put("/sinks/"+info.ID, info, nil)
}

func (c *Host) RemoveSink(id string) error {
	if id == "" {
		return errors.New("missing ID")
	}
	return c.c.Delete("/sinks/" + id)
}
