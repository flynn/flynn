package main

import (
	"path/filepath"
	"testing"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/host/types"
)

func Test(t *testing.T) { TestingT(t) }

type S struct{}

var _ = Suite(&S{})

func (S) TestStateHostID(c *C) {
	workdir := c.MkDir()
	hostID := "abc123"
	state := NewState(hostID, filepath.Join(workdir, "host-state-db"))
	c.Assert(state.OpenDB(), IsNil)
	defer state.CloseDB()
	state.AddJob(&host.Job{ID: "a"}, nil)
	job := state.GetJob("a")
	if job.HostID != hostID {
		c.Errorf("expected job.HostID to equal %s, got %s", hostID, job.HostID)
	}
}

type MockBackend struct{}

func (MockBackend) Run(*host.Job, *RunConfig) error                 { return nil }
func (MockBackend) Stop(string) error                               { return nil }
func (MockBackend) Signal(string, int) error                        { return nil }
func (MockBackend) ResizeTTY(id string, height, width uint16) error { return nil }
func (MockBackend) Attach(*AttachRequest) error                     { return nil }
func (MockBackend) Cleanup([]string) error                          { return nil }
func (MockBackend) SetDefaultEnv(k, v string)                       {}
func (MockBackend) UnmarshalState(map[string]*host.ActiveJob, map[string][]byte, []byte) error {
	return nil
}
func (MockBackend) ConfigureNetworking(*host.NetworkConfig) error { return nil }

func (S) TestStatePersistRestore(c *C) {
	workdir := c.MkDir()
	hostID := "abc123"
	state := NewState(hostID, filepath.Join(workdir, "host-state-db"))
	c.Assert(state.OpenDB(), IsNil)
	state.AddJob(&host.Job{ID: "a"}, nil)
	state.CloseDB()

	// exercise the restore path.  failures will panic.
	// note that this does not test backend deserialization (the mock, obviously, isn't doing anything).
	state = NewState(hostID, filepath.Join(workdir, "host-state-db"))
	c.Assert(state.OpenDB(), IsNil)
	defer state.CloseDB()
	state.Restore(&MockBackend{})

	// check we actually got job data back
	job := state.GetJob("a")
	if job.HostID != hostID {
		c.Errorf("expected job.HostID to equal %s, got %s", hostID, job.HostID)
	}
}
