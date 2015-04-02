package backend

import (
	"fmt"
	"io"
	"net"

	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume/manager"
)

var backends = map[string]func(*State, *volumemanager.Manager, string, string, string, *logmux.LogMux) (Backend, error){}

func New(backendName string, state *State, vman *volumemanager.Manager, volPath, logPath, initPath string, mux *logmux.LogMux) (Backend, error) {
	if fn := backends[backendName]; fn != nil {
		return fn(state, vman, volPath, logPath, initPath, mux)
	}
	return nil, fmt.Errorf("unknown backend %q", backendName)
}

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
	Run(*host.Job, *RunConfig) error
	Stop(string) error
	Signal(string, int) error
	ResizeTTY(id string, height, width uint16) error
	Attach(*AttachRequest) error
	Cleanup() error
	UnmarshalState(map[string]*host.ActiveJob, map[string][]byte, []byte) error
	ConfigureNetworking(strategy NetworkStrategy, job string) (*NetworkInfo, error)
}

type RunConfig struct {
	IP         net.IP
	ManifestID string
}

type NetworkInfo struct {
	BridgeAddr  string
	Nameservers []string
}

type JobStateSaver interface {
	MarshalJobState(jobID string) ([]byte, error)
}

type StateSaver interface {
	MarshalGlobalState() ([]byte, error)
}

type NetworkStrategy int

const (
	NetworkStrategyFlannel NetworkStrategy = iota
)

type ExitError int

func (e ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e)
}
