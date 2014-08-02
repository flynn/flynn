package sampi

import (
	"testing"

	"github.com/flynn/flynn-host/types"
)

func TestStateCommit(t *testing.T) {
	state := NewState()
	if !addHost("foo", state) {
		t.Error("Expected to find 'foo' in commit result")
	}

	if _, ok := state.Get()["foo"]; !ok {
		t.Error("Expected to find 'foo' in current state")
	}
}

func addHost(id string, state *State) bool {
	state.Begin()
	state.AddHost(&host.Host{ID: id}, nil)
	res := state.Commit()
	_, ok := res[id]
	return ok
}

func TestStateRace(t *testing.T) {
	state := NewState()
	state.Begin()
	state.AddHost(&host.Host{ID: "foo"}, nil)
	data := state.Commit()

	go addHost("1", state)
	go addHost("2", state)

	if _, ok := data["foo"]; !ok {
		t.Error("Expected to find 'foo' in commit result")
	}

	if _, ok := data["1"]; ok {
		t.Log("Got '1'")
	}
	if _, ok := data["2"]; ok {
		t.Log("Got '2'")
	}
}
