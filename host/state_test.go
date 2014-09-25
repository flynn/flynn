package main

import (
	"testing"

	"github.com/flynn/flynn/host/types"
)

func TestStateHostID(t *testing.T) {
	hostID := "abc123"
	state := NewState(hostID)
	state.AddJob(&host.Job{ID: "a"})
	job := state.GetJob("a")
	if job.HostID != hostID {
		t.Errorf("expected job.HostID to equal %s, got %s", hostID, job.HostID)
	}
}
