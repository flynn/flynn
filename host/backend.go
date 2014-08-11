package main

import (
	"encoding/json"
	"io"

	"github.com/flynn/flynn/host/types"
)

type AttachRequest struct {
	Job    *host.ActiveJob
	Logs   bool
	Stream bool
	Height uint16
	Width  uint16

	Attached chan struct{}

	Stdout io.WriteCloser
	Stderr io.WriteCloser
	Stdin  io.Reader
}

type Backend interface {
	Run(*host.Job) error
	Stop(string) error
	Signal(string, int) error
	ResizeTTY(id string, height, width uint16) error
	Attach(*AttachRequest) error
	Cleanup() error
	RestoreState(map[string]*host.ActiveJob, *json.Decoder) error
}

type StateSaver interface {
	SaveState(*json.Encoder) error
}
