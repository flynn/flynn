package main

import (
	"fmt"

	"github.com/flynn/flynn/host/logmux"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume/manager"
	"github.com/flynn/flynn/pinkerton"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/libcontainer"
)

type libcontainerBackend struct {
	factory libcontainer.Factory
}

func NewLibcontainerBackend(state *State, vman *volumemanager.Manager, volPath, logPath, initPath string, mux *logmux.LogMux) (Backend, error) {
	factory, err := libcontainer.New("/var/lib/flynn")
	if err != nil {
		return nil, err
	}

	pinkertonCtx, err := pinkerton.BuildContext("aufs", imageRoot)
	if err != nil {
		return nil, err
	}

	fmt.Printf("factory=%#v\n", factory)
	fmt.Printf("pinkertonCtx=%#v\n", pinkertonCtx)

	return &libcontainerBackend{
		factory: factory,
	}, nil
}

func (l *libcontainerBackend) Attach(req *AttachRequest) error {
	panic("TODO")
	return nil
}

func (l *libcontainerBackend) Cleanup() error {
	panic("TODO")
	return nil
}

func (l *libcontainerBackend) ConfigureNetworking(strategy NetworkStrategy, job string) (*NetworkInfo, error) {
	panic("TODO")
	return nil, nil
}

func (l *libcontainerBackend) ResizeTTY(id string, height, width uint16) error {
	panic("TODO")
	return nil
}

func (l *libcontainerBackend) Run(job *host.Job, runConfig *RunConfig) error {
	panic("TODO")
	return nil
}

func (l *libcontainerBackend) Signal(id string, sig int) error {
	panic("TODO")
	return nil
}

func (c *libcontainerBackend) Stop(id string) error {
	panic("TODO")
	return nil
}

func (l *libcontainerBackend) UnmarshalState(jobs map[string]*host.ActiveJob, jobBackendStates map[string][]byte, backendGlobalState []byte) error {
	panic("TODO")
	return nil
}
