package main

import (
	"io"

	"github.com/flynn/flynn-host/types"
)

type AttachRequest struct {
	Job     *host.ActiveJob
	Streams []string
	Logs    bool
	Stream  bool
	Height  int
	Width   int

	// If set, after a successful connect, a sentinel will be sent and then the
	// client will block on receive before continuing.
	Attached chan struct{}

	io.ReadWriter
}

type Backend interface {
	Run(*host.Job) error
	Stop(string) error
	Attach(*AttachRequest) error
	Cleanup() error
}
