package main

import (
	"io"
	"net"

	"github.com/flynn/flynn/host/types"
)

type AttachRequest struct {
	Job    *host.ActiveJob
	Logs   bool
	Stream bool
	Height uint16
	Width  uint16

	Attached chan struct{}

	Stdout  io.WriteCloser
	Stderr  io.WriteCloser
	InitLog io.WriteCloser
	Stdin   io.Reader
}

type Backend interface {
	Run(*host.Job, *RunConfig, *RateLimitBucket) error
	Stop(string) error
	JobExists(id string) bool
	Signal(string, int) error
	DiscoverdDeregister(string) error
	ResizeTTY(id string, height, width uint16) error
	Attach(*AttachRequest) error
	Cleanup([]string) error
	UnmarshalState(map[string]*host.ActiveJob, map[string][]byte, []byte, host.LogBuffers) error
	ConfigureNetworking(config *host.NetworkConfig) error
	SetHost(*Host)
	SetDefaultEnv(k, v string)
	SetDiscoverdConfig(*host.DiscoverdConfig)
	SetNetworkConfig(*host.NetworkConfig)
	OpenLogs(host.LogBuffers) error
	CloseLogs() (host.LogBuffers, error)
}

type RunConfig struct {
	IP net.IP
}

type JobStateSaver interface {
	MarshalJobState(jobID string) ([]byte, error)
}

type StateSaver interface {
	MarshalGlobalState() ([]byte, error)
}

// MockBackend is used when testing flynn-host without the need to actually run jobs
type MockBackend struct{}

func (MockBackend) Run(*host.Job, *RunConfig, *RateLimitBucket) error { return nil }
func (MockBackend) Stop(string) error                                 { return nil }
func (MockBackend) JobExists(string) bool                             { return false }
func (MockBackend) Signal(string, int) error                          { return nil }
func (MockBackend) DiscoverdDeregister(string) error                  { return nil }
func (MockBackend) ResizeTTY(id string, height, width uint16) error   { return nil }
func (MockBackend) Attach(*AttachRequest) error                       { return nil }
func (MockBackend) Cleanup([]string) error                            { return nil }
func (MockBackend) SetDefaultEnv(k, v string)                         {}
func (MockBackend) ConfigureNetworking(*host.NetworkConfig) error     { return nil }
func (MockBackend) OpenLogs(host.LogBuffers) error                    { return nil }
func (MockBackend) CloseLogs() (host.LogBuffers, error)               { return nil, nil }
func (MockBackend) SetDiscoverdConfig(*host.DiscoverdConfig)          {}
func (MockBackend) SetNetworkConfig(*host.NetworkConfig)              {}
func (MockBackend) SetHost(*Host)                                     {}
func (MockBackend) UnmarshalState(map[string]*host.ActiveJob, map[string][]byte, []byte, host.LogBuffers) error {
	return nil
}
