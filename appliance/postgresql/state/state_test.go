//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.
//
//
// This file is derived from the files in this tree:
// https://github.com/joyent/manatee-state-machine/tree/d441fe941faddb51d6e6237d792dd4d7fae64cc6/test
//
// Copyright (c) 2014, Joyent, Inc.
// Copyright (c) 2015, Prime Directive, Inc.
//

package state_test

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/kylelemons/godebug/pretty"
	"github.com/flynn/flynn/appliance/postgresql/simulator"
	"github.com/flynn/flynn/appliance/postgresql/state"
	"github.com/flynn/flynn/appliance/postgresql/xlog"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/iotool"
)

type step struct {
	Cmd   string
	Check interface{}
	JSON  interface{}
}

var fakeTime = time.Now().UTC()

func init() {
	state.TimeNow = func() time.Time { return fakeTime }
}

func runSteps(t *testing.T, singleton bool, steps []step) {
	logOut := &bytes.Buffer{}
	logOut.WriteByte('\n')
	logW := &iotool.SafeWriter{W: logOut}
	defer func() {
		logW.SetWriter(nil) // disable writes to avoid races
		t.Log(logOut)
	}()

	dataOut := &bytes.Buffer{}
	sim := simulator.New(singleton, dataOut, logW)
	defer sim.Close()

	for _, step := range steps {
		if step.JSON != nil {
			jsonData, _ := json.Marshal(step.JSON)
			step.Cmd = fmt.Sprintf("%s %s", step.Cmd, string(jsonData))
		}

		fmt.Fprintf(logW, "== %s\n", step.Cmd)
		sim.RunCommand(step.Cmd)
		if step.Check != nil {
			actual := reflect.New(reflect.Indirect(reflect.ValueOf(step.Check)).Type()).Interface()
			if err := json.Unmarshal(dataOut.Bytes(), actual); err != nil {
				t.Fatal("json decode error", err)
			}
			if diff := pretty.Compare(step.Check, actual); diff != "" {
				t.Fatalf("check failed:\n%s", diff)
			}
		}
		dataOut.Reset()
	}
}

func node(n int, index uint64) *discoverd.Instance {
	inst := &discoverd.Instance{
		Addr:  fmt.Sprintf("10.0.0.%d:5432", n),
		Proto: "tcp",
		Meta:  map[string]string{"name": fmt.Sprintf("node%d", n)},
		Index: index,
	}
	inst.ID = md5sum(inst.Proto + "-" + inst.Addr)
	return inst
}

func md5sum(data string) string {
	digest := md5.Sum([]byte(data))
	return hex.EncodeToString(digest[:])
}

var node1ID = node(1, 1).ID

var pgOffline = &simulator.PostgresInfo{
	Online: false,
	Config: &state.PgConfig{Role: state.RoleNone},
	XLog:   xlog.Zero,
}

// tests the basic flow of unassigned -> async -> sync -> primary
func TestBasic(t *testing.T) {
	gen1 := &state.State{
		Generation: 1,
		Primary:    node(2, 1),
		Sync:       node(3, 2),
		Async:      []*discoverd.Instance{node(1, 3)},
		InitWAL:    xlog.Zero,
	}
	peers := []*discoverd.Instance{node(2, 1), node(3, 2), node(1, 3)}

	gen2_0 := &state.State{
		Generation: 2,
		InitWAL:    "0/0000000A",
		Primary:    node(3, 2),
		Sync:       node(1, 3),
		Deposed:    []*discoverd.Instance{node(2, 1)},
	}
	gen2_1 := &state.State{
		Generation: 2,
		InitWAL:    "0/0000000A",
		Primary:    node(3, 2),
		Sync:       node(1, 3),
		Async:      []*discoverd.Instance{node(2, 1)},
		Deposed:    []*discoverd.Instance{},
	}

	pgSync := &simulator.PostgresInfo{
		Config: &state.PgConfig{
			Role:     state.RoleSync,
			Upstream: node(3, 2),
		},
		Online:      true,
		XLog:        xlog.Zero,
		XLogWaiting: "0/0000000A",
	}
	pgSync2 := &simulator.PostgresInfo{
		Config: &state.PgConfig{
			Role:       state.RoleSync,
			Upstream:   node(3, 2),
			Downstream: node(2, 1),
		},
		Online:      true,
		XLog:        xlog.Zero,
		XLogWaiting: "0/0000000A",
	}
	pgPrimary := &simulator.PostgresInfo{
		Config: &state.PgConfig{
			Role:       state.RolePrimary,
			Downstream: node(2, 1),
		},
		Online: true,
		XLog:   "0/0000001E",
	}
	pgPrimary2 := &simulator.PostgresInfo{
		Config: &state.PgConfig{
			Role:       state.RolePrimary,
			Downstream: node(3, 4),
		},
		Online: true,
		XLog:   "0/00000028",
	}

	runSteps(t, false, []step{
		// validate initial state
		{Cmd: "echo test: validating initial state"},
		{
			Cmd: "discoverd",
			Check: &simulator.DiscoverdInfo{
				State: &state.DiscoverdState{Index: 0},
				Peers: []*discoverd.Instance{},
			},
		},

		// validate addpeer
		{Cmd: "echo test: addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer node1"},
		{
			Cmd: "discoverd",
			Check: &simulator.DiscoverdInfo{
				State: &state.DiscoverdState{Index: 0},
				Peers: peers,
			},
		},

		// validate bootstrap
		{Cmd: "echo test: bootstrap"},
		{Cmd: "bootstrap node2 node3"},
		{
			Cmd: "discoverd",
			Check: &simulator.DiscoverdInfo{
				State: &state.DiscoverdState{Index: 1, State: gen1},
				Peers: peers,
			},
		},

		// validate initial peer state
		{Cmd: "echo test: initial peer state"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer:     &state.PeerInfo{ID: node1ID},
				Postgres: &simulator.PostgresInfo{XLog: xlog.Zero},
			},
		},

		// bring up the peer as an async
		{Cmd: "echo test: start up as async"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleAsync,
					Peers: peers,
					State: gen1,
				},
				Postgres: &simulator.PostgresInfo{
					Config: &state.PgConfig{
						Role:     state.RoleAsync,
						Upstream: node(3, 2),
					},
					Online: true,
					XLog:   xlog.Zero,
				},
			},
		},

		// depose the primary and make sure that our peer comes up as the sync
		{Cmd: "echo test: depose"},
		{Cmd: "depose"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleSync,
					Peers: peers,
					State: gen2_0,
				},
				Postgres: pgSync,
			},
		},

		// rebuild the deposed sync and ensure it shows up in the state
		{Cmd: "echo test: rebuild"},
		{Cmd: "rebuild node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleSync,
					Peers: peers,
					State: gen2_1,
				},
				Postgres: pgSync2,
			},
		},

		// At this point if the primary fails, our peer should not take over
		// because it has not caught up.
		{Cmd: "echo test: no catch up and remove primary"},
		{Cmd: "rmpeer node3 retrylater"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleSync,
					Peers: []*discoverd.Instance{node(2, 1), node(1, 3)},
					State: gen2_1,
				},
				Postgres: pgSync2,
			},
		},
		{Cmd: "addpeer node3"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleSync,
					Peers: []*discoverd.Instance{node(2, 1), node(1, 3), node(3, 4)},
					State: gen2_1,
				},
				Postgres: pgSync2,
			},
		},

		// Now if we issue a catchUp and do the same thing, our peer should
		// become the primary.
		{Cmd: "echo test: catch up and remove primary"},
		{Cmd: "catchup"},
		{Cmd: "rmpeer node3"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(2, 1), node(1, 3)},
					State: &state.State{
						Generation: 3,
						InitWAL:    "0/00000014",
						Primary:    node(1, 3),
						Sync:       node(2, 1),
						Deposed:    []*discoverd.Instance{node(3, 2)},
					},
				},
				Postgres: pgPrimary,
			},
		},
		{Cmd: "rebuild node3"},
		{Cmd: "addpeer node3"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(2, 1), node(1, 3), node(3, 4)},
					State: &state.State{
						Generation: 3,
						InitWAL:    "0/00000014",
						Primary:    node(1, 3),
						Sync:       node(2, 1),
						Async:      []*discoverd.Instance{node(3, 4)},
					},
				},
				Postgres: pgPrimary,
			},
		},

		// Now have the sync fail. Our peer should promote the async.
		{Cmd: "echo test: sync fail"},
		{Cmd: "rmpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(1, 3), node(3, 4)},
					State: &state.State{
						Generation: 4,
						InitWAL:    "0/0000001E",
						Primary:    node(1, 3),
						Sync:       node(3, 4),
					},
				},
				Postgres: pgPrimary2,
			},
		},
		{Cmd: "addpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(1, 3), node(3, 4), node(2, 5)},
					State: &state.State{
						Generation: 4,
						InitWAL:    "0/0000001E",
						Primary:    node(1, 3),
						Sync:       node(3, 4),
						Async:      []*discoverd.Instance{node(2, 5)},
					},
				},
				Postgres: pgPrimary2,
			},
		},

		// Test adding a new async peer
		{Cmd: "echo test: add async"},
		{Cmd: "addpeer node4"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(1, 3), node(3, 4), node(2, 5), node(4, 6)},
					State: &state.State{
						Generation: 4,
						InitWAL:    "0/0000001E",
						Primary:    node(1, 3),
						Sync:       node(3, 4),
						Async:      []*discoverd.Instance{node(2, 5), node(4, 6)},
					},
				},
				Postgres: pgPrimary2,
			},
		},

		// Test out removing an async peer
		{Cmd: "echo test: remove async"},
		{Cmd: "rmpeer node4"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(1, 3), node(3, 4), node(2, 5)},
					State: &state.State{
						Generation: 4,
						InitWAL:    "0/0000001E",
						Primary:    node(1, 3),
						Sync:       node(3, 4),
						Async:      []*discoverd.Instance{node(2, 5)},
					},
				},
				Postgres: pgPrimary2,
			},
		},

		// Finally, have someone depose our peer
		{Cmd: "echo test: primary deposed"},
		{Cmd: "depose"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleDeposed,
					Peers: []*discoverd.Instance{node(1, 3), node(3, 4), node(2, 5)},
					State: &state.State{
						Generation: 5,
						InitWAL:    "0/00000028",
						Primary:    node(3, 4),
						Sync:       node(2, 5),
						Deposed:    []*discoverd.Instance{node(1, 3)},
					},
				},
				Postgres: &simulator.PostgresInfo{
					Online: false,
					Config: &state.PgConfig{Role: state.RoleNone},
					XLog:   "0/00000028",
				},
			},
		},
	})
}

// tests cluster setup when no peers are initially present.
func TestClusterSetupDelay(t *testing.T) {
	runSteps(t, false, []step{
		// Test that when the peer comes up with no other peers, we wait on
		// cluster setup
		{Cmd: "echo test: start up with no peers"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleUnassigned,
					Peers: []*discoverd.Instance{node(1, 1)},
				},
				Postgres: pgOffline,
			},
		},

		// When we add a peer, we create a cluster
		{Cmd: "addpeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(1, 1), node(2, 2)},
					State: &state.State{
						Generation: 1,
						Primary:    node(1, 1),
						Sync:       node(2, 2),
						InitWAL:    xlog.Zero,
					},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(2, 2),
					},
					XLog: "0/0000000A",
				},
			},
		},
	})
}

// Test cluster setup when another peer is already present
func TestClusterSetupImmediate(t *testing.T) {
	runSteps(t, false, []step{
		// Test that when the peer comes up with other peers present and we're
		// the first peer, we initiate the cluster setup.
		{Cmd: "echo test: start up with other peers"},
		{Cmd: "addpeer node1"},
		{Cmd: "addpeer"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(1, 1), node(2, 2)},
					State: &state.State{
						Generation: 1,
						Primary:    node(1, 1),
						Sync:       node(2, 2),
						InitWAL:    xlog.Zero,
					},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(2, 2),
					},
					XLog: "0/0000000A",
				},
			},
		},
	})
}

// Test cluster setup in singleton mode
func TestClusterSetupSingleton(t *testing.T) {
	runSteps(t, true, []step{
		{Cmd: "echo: test: start up in singleton mode"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(1, 1)},
					State: &state.State{
						Generation: 1,
						Primary:    node(1, 1),
						InitWAL:    xlog.Zero,
						Singleton:  true,
						Freeze: &state.FreezeDetails{
							Reason:   "cluster started in singleton mode",
							FrozenAt: fakeTime,
						},
					},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{Role: state.RolePrimary},
					XLog:   "0/0000000A",
				},
			},
		},
	})
}

// Test cluster setup from the perspective of the second peer
func TestSetupPassive(t *testing.T) {
	runSteps(t, false, []step{
		// Test that when the peer comes up with other peers present but we're
		// not first, we wait on cluster setup.
		{Cmd: "echo test: start up with other peers"},
		{Cmd: "addpeer"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleUnassigned,
					Peers: []*discoverd.Instance{node(2, 1), node(1, 2)},
				},
				Postgres: pgOffline,
			},
		},

		// Now simulate the other peer setting up the cluster
		{Cmd: "bootstrap"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleSync,
					Peers: []*discoverd.Instance{node(2, 1), node(1, 2)},
					State: &state.State{
						Generation: 1,
						Primary:    node(2, 1),
						Sync:       node(1, 2),
						InitWAL:    xlog.Zero,
					},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:     state.RoleSync,
						Upstream: node(2, 1),
					},
					XLog:        xlog.Zero,
					XLogWaiting: xlog.Zero,
				},
			},
		},
	})
}

// Test several cases where we shouldn't takeover
func TestNoFlap(t *testing.T) {
	gen1 := &state.State{
		Generation: 1,
		Primary:    node(2, 1),
		Sync:       node(1, 2),
		InitWAL:    xlog.Zero,
	}
	gen1pg := &simulator.PostgresInfo{
		Online: true,
		Config: &state.PgConfig{
			Role:     state.RoleSync,
			Upstream: node(2, 1),
		},
		XLog:        xlog.Zero,
		XLogWaiting: xlog.Zero,
	}

	gen2_0 := &state.State{
		Generation: 2,
		Primary:    node(1, 2),
		Sync:       node(3, 3),
		Deposed:    []*discoverd.Instance{node(2, 1)},
		InitWAL:    xlog.Zero,
	}
	gen2_1 := &state.State{
		Generation: 2,
		Primary:    node(1, 2),
		Sync:       node(3, 3),
		InitWAL:    xlog.Zero,
	}
	gen2pg := &simulator.PostgresInfo{
		Online: true,
		Config: &state.PgConfig{
			Role:       state.RolePrimary,
			Downstream: node(3, 3),
		},
		XLog: "0/0000000A",
	}

	runSteps(t, false, []step{
		// Bring up our peer as a sync and make sure it doesn't take over when
		// there are no asyncs available.
		{Cmd: "echo test: no takeover without asyncs (as sync)"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer node1"},
		{Cmd: "bootstrap node2 node1"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleSync,
					Peers: []*discoverd.Instance{node(2, 1), node(1, 2)},
					State: gen1,
				},
				Postgres: gen1pg,
			},
		},
		{Cmd: "rmpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleSync,
					Peers: []*discoverd.Instance{node(1, 2)},
					State: gen1,
				},
				Postgres: gen1pg,
			},
		},

		// Now allow the peer to takeover by adding a new async
		{Cmd: "addpeer"}, // this also adds to the async list in the state
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(1, 2), node(3, 3)},
					State: gen2_0,
				},
				Postgres: gen2pg,
			},
		},
		{Cmd: "rebuild node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(1, 2), node(3, 3)},
					State: gen2_1,
				},
				Postgres: gen2pg,
			},
		},

		// Make sure we don't declare a new generation when the sync fails
		// because there is no async.
		{Cmd: "rmpeer node3"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(1, 2)},
					State: gen2_1,
				},
				Postgres: gen2pg,
			},
		},
	})
}

// Test operation in singleton mode
func TestSingleton(t *testing.T) {
	gen1 := &state.State{
		Generation: 1,
		Primary:    node(1, 1),
		InitWAL:    xlog.Zero,
		Freeze:     state.NewFreezeDetails("singleton"),
		Singleton:  true,
	}
	gen1pg := &simulator.PostgresInfo{
		Online: true,
		Config: &state.PgConfig{Role: state.RolePrimary},
		XLog:   "0/0000000A",
	}

	info := &simulator.PeerSimInfo{
		Peer: &state.PeerInfo{
			ID:    node1ID,
			Role:  state.RolePrimary,
			Peers: []*discoverd.Instance{node(1, 1)},
			State: gen1,
		},
		Postgres: gen1pg,
	}

	runSteps(t, true, []step{
		// Test starting up in singleton mode
		{Cmd: "echo test: start up as primary in singleton mode"},
		{Cmd: "setClusterState", JSON: gen1},
		{Cmd: "startPeer"},
		{Cmd: "peer", Check: info},

		// Test not doing anything when another peer shows up
		{Cmd: "echo test: do nothing with new peers"},
		{Cmd: "addpeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					Peers: []*discoverd.Instance{node(1, 1), node(2, 2)},
					State: gen1,
				},
				Postgres: gen1pg,
			},
		},
		{Cmd: "rmpeer node2"},
		{Cmd: "peer", Check: info},
	})
}

// Test what happens when we show up as the second node in singleton mode
func TestSingletonSecond(t *testing.T) {
	gen1 := &state.State{
		Generation: 1,
		Primary:    node(2, 1),
		InitWAL:    xlog.Zero,
		Freeze:     state.NewFreezeDetails("singleton"),
		Singleton:  true,
	}

	runSteps(t, true, []step{
		// Test that we don't do anything if we start up in singleton mode when
		// another peer is the primary
		{Cmd: "echo test: start in singleton mode"},
		{Cmd: "addpeer"},
		{Cmd: "setclusterstate", JSON: gen1},
		{Cmd: "startpeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleUnassigned,
					State: gen1,
					Peers: []*discoverd.Instance{node(2, 1), node(1, 2)},
				},
				Postgres: pgOffline,
			},
		},

		// Check that we don't do anything even if the primary fails
		{Cmd: "echo test: do nothing even if primary fails"},
		{Cmd: "rmpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleUnassigned,
					State: gen1,
					Peers: []*discoverd.Instance{node(1, 2)},
				},
				Postgres: pgOffline,
			},
		},
	})
}

// Test upgrading from singleton to normal mode
func TestSingletonUpgradeToNormal(t *testing.T) {
	gen1 := &state.State{
		Generation: 1,
		Primary:    node(1, 1),
		InitWAL:    xlog.Zero,
		Freeze:     state.NewFreezeDetails("singleton"),
		Singleton:  true,
	}

	runSteps(t, false, []step{
		// Start in singleton mode
		{Cmd: "echo test: start up as primary in singleton mode"},
		{Cmd: "setclusterstate", JSON: gen1},
		{Cmd: "startpeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					State: gen1,
					Peers: []*discoverd.Instance{node(1, 1)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{Role: state.RolePrimary},
					XLog:   "0/0000000A",
				},
			},
		},

		// Unfreeze the cluster and make sure we do nothing
		{Cmd: "echo test: unfreeze"},
		{Cmd: "unfreeze"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 1,
						Primary:    node(1, 1),
						InitWAL:    xlog.Zero,
						Singleton:  true,
					},
					Peers: []*discoverd.Instance{node(1, 1)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{Role: state.RolePrimary},
					XLog:   "0/0000000A",
				},
			},
		},

		// Add another peer and see that we transition to normal mode
		{Cmd: "echo test: transition to normal mode"},
		{Cmd: "addpeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 2,
						Primary:    node(1, 1),
						Sync:       node(2, 2),
						InitWAL:    "0/0000000A",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(2, 2)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(2, 2),
					},
					XLog: "0/00000014",
				},
			},
		},

		// Add another peer, fail the sync, and make sure we reconfigure
		// appropriately.
		{Cmd: "echo test: reconfiguration in normal mode"},
		{Cmd: "addpeer"},
		{Cmd: "rmpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 3,
						Primary:    node(1, 1),
						Sync:       node(3, 3),
						InitWAL:    "0/00000014",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(3, 3)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(3, 3),
					},
					XLog: "0/0000001E",
				},
			},
		},
		{Cmd: "addpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 3,
						Primary:    node(1, 1),
						Sync:       node(3, 3),
						Async:      []*discoverd.Instance{node(2, 4)},
						InitWAL:    "0/00000014",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(3, 3), node(2, 4)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(3, 3),
					},
					XLog: "0/0000001E",
				},
			},
		},
	})
}

// Test starting as a deposed peer
func TestStartDeposed(t *testing.T) {
	gen2 := &state.State{
		Generation: 2,
		Primary:    node(2, 2),
		Sync:       node(3, 3),
		Deposed:    []*discoverd.Instance{node(1, 1)},
		InitWAL:    "0/0000000A",
	}
	peers := []*discoverd.Instance{node(1, 1), node(2, 2), node(3, 3)}

	runSteps(t, false, []step{
		{Cmd: "addpeer node1"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "bootstrap"},
		{Cmd: "depose"},
		{
			Cmd: "discoverd",
			Check: &simulator.DiscoverdInfo{
				State: &state.DiscoverdState{Index: 2, State: gen2},
				Peers: peers,
			},
		},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleDeposed,
					State: gen2,
					Peers: peers,
				},
				Postgres: pgOffline,
			},
		},
	})
}

// Test starting as the primary peer
func TestStartPrimary(t *testing.T) {
	gen1 := &state.State{
		Generation: 1,
		Primary:    node(1, 1),
		Sync:       node(2, 2),
		Async:      []*discoverd.Instance{node(3, 3)},
		InitWAL:    xlog.Zero,
	}
	gen1peers := []*discoverd.Instance{node(1, 1), node(2, 2), node(3, 3)}
	gen3pg := &simulator.PostgresInfo{
		Online: true,
		Config: &state.PgConfig{
			Role:       state.RolePrimary,
			Downstream: node(3, 3),
		},
		XLog: "0/00000014",
	}

	runSteps(t, false, []step{
		// Start as the primary
		{Cmd: "echo test: start as primary"},
		{Cmd: "addpeer node1"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "bootstrap"},
		{
			Cmd: "discoverd",
			Check: &simulator.DiscoverdInfo{
				State: &state.DiscoverdState{
					Index: 1,
					State: gen1,
				},
				Peers: gen1peers,
			},
		},
		{Cmd: "startpeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					State: gen1,
					Peers: gen1peers,
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(2, 2),
					},
					XLog: "0/0000000A",
				},
			},
		},

		// Now have the sync fail. Our peer should promote the async.
		{Cmd: "echo test: sync fail"},
		{Cmd: "rmpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 2,
						Primary:    node(1, 1),
						Sync:       node(3, 3),
						InitWAL:    "0/0000000A",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(3, 3)},
				},
				Postgres: gen3pg,
			},
		},
		{Cmd: "addpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 2,
						Primary:    node(1, 1),
						Sync:       node(3, 3),
						Async:      []*discoverd.Instance{node(2, 4)},
						InitWAL:    "0/0000000A",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(3, 3), node(2, 4)},
				},
				Postgres: gen3pg,
			},
		},
	})
}

// Test starting the primary when the sync is not up but the async is. This
// simulates a cluster reboot where the sync comes up last.
func TestStartPrimaryAsync(t *testing.T) {
	runSteps(t, false, []step{
		{Cmd: "addpeer node1"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "bootstrap node1 node2"},
		{
			Cmd: "discoverd",
			Check: &simulator.DiscoverdInfo{
				State: &state.DiscoverdState{
					Index: 1,
					State: &state.State{
						Generation: 1,
						Primary:    node(1, 1),
						Sync:       node(2, 2),
						Async:      []*discoverd.Instance{node(3, 3)},
						InitWAL:    xlog.Zero,
					},
				},
				Peers: []*discoverd.Instance{node(1, 1), node(2, 2), node(3, 3)},
			},
		},
		{Cmd: "rmpeer node2"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 2,
						Primary:    node(1, 1),
						Sync:       node(3, 3),
						InitWAL:    "0/0000000A",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(3, 3)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(3, 3),
					},
					XLog: "0/00000014",
				},
			},
		},
	})
}

// Test starting as a sync peer
func TestStartSync(t *testing.T) {
	gen1 := &state.State{
		Generation: 1,
		Primary:    node(3, 3),
		Sync:       node(1, 1),
		Async:      []*discoverd.Instance{node(2, 2)},
		InitWAL:    xlog.Zero,
	}
	gen1peers := []*discoverd.Instance{node(1, 1), node(2, 2), node(3, 3)}

	gen2pg := &simulator.PostgresInfo{
		Online: true,
		Config: &state.PgConfig{
			Role:       state.RolePrimary,
			Downstream: node(2, 2),
		},
		XLog: "0/00000014",
	}
	gen3pg := &simulator.PostgresInfo{
		Online: true,
		Config: &state.PgConfig{
			Role:       state.RolePrimary,
			Downstream: node(3, 3),
		},
		XLog: "0/0000001E",
	}

	runSteps(t, false, []step{
		// Validate bootstrap
		{Cmd: "echo test: bootstrap"},
		{Cmd: "addpeer node1"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "bootstrap node3"},
		{
			Cmd: "discoverd",
			Check: &simulator.DiscoverdInfo{
				State: &state.DiscoverdState{Index: 1, State: gen1},
				Peers: gen1peers,
			},
		},

		// Bring up the peer as the sync
		{Cmd: "startpeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleSync,
					State: gen1,
					Peers: gen1peers,
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RoleSync,
						Upstream:   node(3, 3),
						Downstream: node(2, 2),
					},
					XLog:        xlog.Zero,
					XLogWaiting: xlog.Zero,
				},
			},
		},

		// Catch up, kill the primary, and our peer should become primary
		{Cmd: "echo test: catch up and remove primary"},
		{Cmd: "catchUp"},
		{Cmd: "rmpeer node3"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 2,
						Primary:    node(1, 1),
						Sync:       node(2, 2),
						Deposed:    []*discoverd.Instance{node(3, 3)},
						InitWAL:    "0/0000000A",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(2, 2)},
				},
				Postgres: gen2pg,
			},
		},
		{Cmd: "rebuild node3"},
		{Cmd: "addpeer node3"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 2,
						Primary:    node(1, 1),
						Sync:       node(2, 2),
						Async:      []*discoverd.Instance{node(3, 3)},
						InitWAL:    "0/0000000A",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(2, 2), node(3, 3)},
				},
				Postgres: gen2pg,
			},
		},

		// Now have the sync fail. Our peer should promote the async.
		{Cmd: "echo test: sync fail"},
		{Cmd: "rmpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 3,
						Primary:    node(1, 1),
						Sync:       node(3, 3),
						InitWAL:    "0/00000014",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(3, 3)},
				},
				Postgres: gen3pg,
			},
		},
		{Cmd: "addpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 3,
						Primary:    node(1, 1),
						Sync:       node(3, 3),
						Async:      []*discoverd.Instance{node(2, 4)},
						InitWAL:    "0/00000014",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(3, 3), node(2, 4)},
				},
				Postgres: gen3pg,
			},
		},
	})
}

// Test starting as the sync when the primary is not up but the async is. This
// simulates a cluster reboot where the primary comes up last.
func TestStartSyncAsync(t *testing.T) {
	runSteps(t, false, []step{
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer node1"},
		{Cmd: "bootstrap node2 node1"},
		{
			Cmd: "discoverd",
			Check: &simulator.DiscoverdInfo{
				State: &state.DiscoverdState{
					Index: 1,
					State: &state.State{
						Generation: 1,
						Primary:    node(2, 1),
						Sync:       node(1, 3),
						Async:      []*discoverd.Instance{node(3, 2)},
						InitWAL:    xlog.Zero,
					},
				},
				Peers: []*discoverd.Instance{node(2, 1), node(3, 2), node(1, 3)},
			},
		},
		{Cmd: "rmpeer node2"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 2,
						Primary:    node(1, 3),
						Sync:       node(3, 2),
						Deposed:    []*discoverd.Instance{node(2, 1)},
						InitWAL:    xlog.Zero,
					},
					Peers: []*discoverd.Instance{node(3, 2), node(1, 3)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(3, 2),
					},
					XLog: "0/0000000A",
				},
			},
		},
	})
}

// Test starting as the sync with no other peers present
func TestStartSyncAlone(t *testing.T) {
	runSteps(t, false, []step{
		{Cmd: "addpeer"},
		{Cmd: "addpeer node1"},
		{Cmd: "bootstrap node2 node1"},
		{Cmd: "rmpeer node2"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RoleSync,
					State: &state.State{
						Generation: 1,
						Primary:    node(2, 1),
						Sync:       node(1, 2),
						InitWAL:    xlog.Zero,
					},
					Peers: []*discoverd.Instance{node(1, 2)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:     state.RoleSync,
						Upstream: node(2, 1),
					},
					XLog:        xlog.Zero,
					XLogWaiting: xlog.Zero,
				},
			},
		},
	})
}

// Test starting with an existing state as the only peer, unassigned
func TestStartUnassigned(t *testing.T) {
	peers := []*discoverd.Instance{node(1, 1), node(2, 2), node(3, 3), node(4, 4)}
	gen1 := &state.State{
		Generation: 1,
		Primary:    node(2, 1),
		Sync:       node(3, 2),
		InitWAL:    xlog.Zero,
	}
	gen1_1 := &state.State{
		Generation: 1,
		Primary:    node(2, 1),
		Sync:       node(3, 3),
		Async:      peers[:1],
		InitWAL:    xlog.Zero,
	}

	runSteps(t, false, []step{
		// Start with an existing state and the nodes referenced all gone
		{Cmd: "echo test: start unassigned"},
		{Cmd: "setClusterState", JSON: gen1},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleUnassigned,
					State: gen1,
					Peers: peers[:1],
				},
				Postgres: pgOffline,
			},
		},

		// Start the nodes in the state, and simulate adding as an async
		{Cmd: "echo test: start and add async"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleUnassigned,
					State: gen1,
					Peers: peers[:3],
				},
				Postgres: pgOffline,
			},
		},
		{Cmd: "setClusterState", JSON: gen1_1},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleAsync,
					State: gen1_1,
					Peers: peers[:3],
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:     state.RoleAsync,
						Upstream: node(3, 3),
					},
					XLog: xlog.Zero,
				},
			},
		},

		// Add another async, and depose the primary and secondary so we end up
		// with node1 as primary and node4 as sync.
		{Cmd: "echo test: depose node2/node3"},
		{Cmd: "addpeer"},
		{Cmd: "depose"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RoleSync,
					State: &state.State{
						Generation: 2,
						Primary:    node(3, 3),
						Sync:       node(1, 1),
						Deposed:    []*discoverd.Instance{node(2, 1)},
						Async:      []*discoverd.Instance{node(4, 4)},
						InitWAL:    "0/0000000A",
					},
					Peers: peers,
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RoleSync,
						Upstream:   node(3, 3),
						Downstream: node(4, 4),
					},
					XLog:        xlog.Zero,
					XLogWaiting: "0/0000000A",
				},
			},
		},
		{Cmd: "catchUp"},
		{Cmd: "rmpeer node2"},
		{Cmd: "rmpeer node3"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 3,
						Primary:    node(1, 1),
						Sync:       node(4, 4),
						Deposed:    []*discoverd.Instance{node(2, 1), node(3, 3)},
						InitWAL:    "0/00000014",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(4, 4)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(4, 4),
					},
					XLog: "0/0000001E",
				},
			},
		},
	})
}

// Test promotion with multiple asyncs when the sync fails
func TestMultiAsync(t *testing.T) {
	peers := []*discoverd.Instance{node(1, 1), node(2, 2), node(3, 3), node(4, 4)}

	runSteps(t, false, []step{
		// start with two asyncs
		{Cmd: "echo test: start"},
		{Cmd: "addpeer node1"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 1,
						Primary:    node(1, 1),
						Sync:       node(2, 2),
						Async:      peers[2:],
						InitWAL:    xlog.Zero,
					},
					Peers: peers,
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(2, 2),
					},
					XLog: "0/0000000A",
				},
			},
		},

		// Removing the sync should result in a new generation
		{Cmd: "echo test: remove sync"},
		{Cmd: "rmpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 2,
						Primary:    node(1, 1),
						Sync:       node(3, 3),
						Async:      peers[3:],
						InitWAL:    "0/0000000A",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(3, 3), node(4, 4)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(3, 3),
					},
					XLog: "0/00000014",
				},
			},
		},

		// Removing the sync again should result in yet another generation
		{Cmd: "echo test: remove sync again"},
		{Cmd: "rmpeer node3"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 3,
						Primary:    node(1, 1),
						Sync:       node(4, 4),
						InitWAL:    "0/00000014",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(4, 4)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(4, 4),
					},
					XLog: "0/0000001E",
				},
			},
		},
	})
}

// Test that no generation changes happen while the cluster is frozen and we are
// the primary.
func TestFreezePrimary(t *testing.T) {
	peers := []*discoverd.Instance{node(1, 1), node(2, 2), node(3, 3)}

	gen1 := &state.State{
		Generation: 1,
		Primary:    node(1, 1),
		Sync:       node(2, 2),
		Async:      peers[2:],
		InitWAL:    xlog.Zero,
	}
	gen1frozen := &state.State{
		Generation: 1,
		Primary:    node(1, 1),
		Sync:       node(2, 2),
		Async:      peers[2:],
		InitWAL:    xlog.Zero,
		Freeze: &state.FreezeDetails{
			FrozenAt: fakeTime,
			Reason:   "frozen by simulator",
		},
	}
	gen1pg := &simulator.PostgresInfo{
		Online: true,
		Config: &state.PgConfig{
			Role:       state.RolePrimary,
			Downstream: node(2, 2),
		},
		XLog: "0/0000000A",
	}
	gen2pg := &simulator.PostgresInfo{
		Online: true,
		Config: &state.PgConfig{
			Role:       state.RolePrimary,
			Downstream: node(3, 3),
		},
		XLog: "0/00000014",
	}

	runSteps(t, false, []step{
		// start a three node cluster and freeze it
		{Cmd: "echo test: start cluster"},
		{Cmd: "addpeer node1"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					State: gen1,
					Peers: peers,
				},
				Postgres: gen1pg,
			},
		},
		{Cmd: "freeze"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					State: gen1frozen,
					Peers: []*discoverd.Instance{node(1, 1), node(2, 2), node(3, 3)},
				},
				Postgres: gen1pg,
			},
		},

		// Add another async and ensure the state does not change
		{Cmd: "echo test: add async"},
		{Cmd: "addpeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					State: gen1frozen,
					Peers: []*discoverd.Instance{node(1, 1), node(2, 2), node(3, 3), node(4, 4)},
				},
				Postgres: gen1pg,
			},
		},

		// Remove the sync and make sure nothing happens
		{Cmd: "echo test: remove sync"},
		{Cmd: "rmpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RolePrimary,
					State: gen1frozen,
					Peers: []*discoverd.Instance{node(1, 1), node(3, 3), node(4, 4)},
				},
				Postgres: gen1pg,
			},
		},

		// Unfreeze and ensure the expected changes are applied
		{Cmd: "echo test: unfreeze with missing sync"},
		{Cmd: "unfreeze"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 2,
						Primary:    node(1, 1),
						Sync:       node(3, 3),
						Async:      []*discoverd.Instance{node(4, 4)},
						InitWAL:    "0/0000000A",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(3, 3), node(4, 4)},
				},
				Postgres: gen2pg,
			},
		},

		// Freeze and remove an async
		{Cmd: "echo test: freeze and remove async"},
		{Cmd: "freeze"},
		{Cmd: "rmpeer node4"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 2,
						Primary:    node(1, 1),
						Sync:       node(3, 3),
						Async:      []*discoverd.Instance{node(4, 4)},
						InitWAL:    "0/0000000A",
						Freeze: &state.FreezeDetails{
							FrozenAt: fakeTime,
							Reason:   "frozen by simulator",
						},
					},
					Peers: []*discoverd.Instance{node(1, 1), node(3, 3)},
				},
				Postgres: gen2pg,
			},
		},

		// Freeze and ensure the async is removed from the state
		{Cmd: "echo test: unfreeze with missing async"},
		{Cmd: "unfreeze"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 2,
						Primary:    node(1, 1),
						Sync:       node(3, 3),
						InitWAL:    "0/0000000A",
					},
					Peers: []*discoverd.Instance{node(1, 1), node(3, 3)},
				},
				Postgres: gen2pg,
			},
		},
	})
}

// Test that no generation changes happen while the cluster is frozen and we are
// the sync.
func TestFreezeSync(t *testing.T) {
	gen1 := &state.State{
		Generation: 1,
		Primary:    node(2, 1),
		Sync:       node(1, 2),
		Async:      []*discoverd.Instance{node(3, 3)},
		InitWAL:    xlog.Zero,
		Freeze: &state.FreezeDetails{
			FrozenAt: fakeTime,
			Reason:   "frozen by simulator",
		},
	}
	gen1pg := &simulator.PostgresInfo{
		Online: true,
		Config: &state.PgConfig{
			Role:       state.RoleSync,
			Upstream:   node(2, 1),
			Downstream: node(3, 3),
		},
		XLog: "0/0000000A",
	}

	runSteps(t, false, []step{
		// start cluster frozen as sync
		{Cmd: "echo test: start cluster"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer node1"},
		{Cmd: "addpeer"},
		{Cmd: "bootstrap node2 node1"},
		{Cmd: "freeze"},
		{Cmd: "startPeer"},
		{Cmd: "catchUp"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleSync,
					State: gen1,
					Peers: []*discoverd.Instance{node(2, 1), node(1, 2), node(3, 3)},
				},
				Postgres: gen1pg,
			},
		},

		// Remove the primary, creating a takeover condition but do nothing due
		// to freeze
		{Cmd: "echo test: remove primary"},
		{Cmd: "rmpeer node2"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:    node1ID,
					Role:  state.RoleSync,
					State: gen1,
					Peers: []*discoverd.Instance{node(1, 2), node(3, 3)},
				},
				Postgres: gen1pg,
			},
		},

		// Unfreeze the cluster and make sure that we takeover
		{Cmd: "echo test: unfreeze"},
		{Cmd: "unfreeze"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RolePrimary,
					State: &state.State{
						Generation: 2,
						Primary:    node(1, 2),
						Sync:       node(3, 3),
						Deposed:    []*discoverd.Instance{node(2, 1)},
						InitWAL:    "0/0000000A",
					},
					Peers: []*discoverd.Instance{node(1, 2), node(3, 3)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RolePrimary,
						Downstream: node(3, 3),
					},
					XLog: "0/00000014",
				},
			},
		},
	})
}

// Test changing our upstream as an async
func TestAsyncChangeUpstream(t *testing.T) {
	runSteps(t, false, []step{
		// start cluster as async with async upstream
		{Cmd: "echo test: start cluster"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer node1"},
		{Cmd: "bootstrap node2 node3"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RoleAsync,
					State: &state.State{
						Generation: 1,
						Primary:    node(2, 1),
						Sync:       node(3, 2),
						Async:      []*discoverd.Instance{node(4, 3), node(5, 4), node(1, 5)},
						InitWAL:    xlog.Zero,
					},
					Peers: []*discoverd.Instance{node(2, 1), node(3, 2), node(4, 3), node(5, 4), node(1, 5)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:     state.RoleAsync,
						Upstream: node(5, 4),
					},
					XLog: xlog.Zero,
				},
			},
		},

		// remove our upstream async
		{Cmd: "echo test: remove upstream async"},
		{Cmd: "rmpeer node5"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RoleAsync,
					State: &state.State{
						Generation: 1,
						Primary:    node(2, 1),
						Sync:       node(3, 2),
						Async:      []*discoverd.Instance{node(4, 3), node(1, 5)},
						InitWAL:    xlog.Zero,
					},
					Peers: []*discoverd.Instance{node(2, 1), node(3, 2), node(4, 3), node(1, 5)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:     state.RoleAsync,
						Upstream: node(4, 3),
					},
					XLog: xlog.Zero,
				},
			},
		},

		// remove our upstream async, making our new upstream the sync
		{Cmd: "echo test: remove upstream again"},
		{Cmd: "rmpeer node4"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RoleAsync,
					State: &state.State{
						Generation: 1,
						Primary:    node(2, 1),
						Sync:       node(3, 2),
						Async:      []*discoverd.Instance{node(1, 5)},
						InitWAL:    xlog.Zero,
					},
					Peers: []*discoverd.Instance{node(2, 1), node(3, 2), node(1, 5)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:     state.RoleAsync,
						Upstream: node(3, 2),
					},
					XLog: xlog.Zero,
				},
			},
		},
	})

}

// Test being removed as async
func TestRemovedAsync(t *testing.T) {
	runSteps(t, false, []step{
		// start cluster as async
		{Cmd: "echo test: start cluster"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer node1"},
		{Cmd: "bootstrap node2 node3"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RoleAsync,
					State: &state.State{
						Generation: 1,
						Primary:    node(2, 1),
						Sync:       node(3, 2),
						Async:      []*discoverd.Instance{node(1, 3)},
						InitWAL:    xlog.Zero,
					},
					Peers: []*discoverd.Instance{node(2, 1), node(3, 2), node(1, 3)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:     state.RoleAsync,
						Upstream: node(3, 2),
					},
					XLog: xlog.Zero,
				},
			},
		},

		// remove from cluster state
		{Cmd: "echo test: remove from cluster state"},
		{Cmd: "rmpeer node1"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RoleUnassigned,
					State: &state.State{
						Generation: 1,
						Primary:    node(2, 1),
						Sync:       node(3, 2),
						InitWAL:    xlog.Zero,
					},
					Peers: []*discoverd.Instance{node(2, 1), node(3, 2), node(1, 3)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: false,
					Config: &state.PgConfig{
						Role: state.RoleNone,
					},
					XLog: xlog.Zero,
				},
			},
		},
	})
}

// Test being removed as sync
func TestRemovedSync(t *testing.T) {
	runSteps(t, false, []step{
		// start cluster as sync
		{Cmd: "echo test: start cluster"},
		{Cmd: "addpeer"},
		{Cmd: "addpeer node1"},
		{Cmd: "addpeer"},
		{Cmd: "bootstrap node2 node1"},
		{Cmd: "startPeer"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RoleSync,
					State: &state.State{
						Generation: 1,
						Primary:    node(2, 1),
						Sync:       node(1, 2),
						Async:      []*discoverd.Instance{node(3, 3)},
						InitWAL:    xlog.Zero,
					},
					Peers: []*discoverd.Instance{node(2, 1), node(1, 2), node(3, 3)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: true,
					Config: &state.PgConfig{
						Role:       state.RoleSync,
						Upstream:   node(2, 1),
						Downstream: node(3, 3),
					},
					XLog:        xlog.Zero,
					XLogWaiting: xlog.Zero,
				},
			},
		},

		// remove from cluster state
		{Cmd: "echo test: remove from cluster state"},
		{Cmd: "rmpeer node1"},
		{
			Cmd: "peer",
			Check: &simulator.PeerSimInfo{
				Peer: &state.PeerInfo{
					ID:   node1ID,
					Role: state.RoleUnassigned,
					State: &state.State{
						Generation: 2,
						Primary:    node(2, 1),
						Sync:       node(3, 3),
						InitWAL:    xlog.Zero,
					},
					Peers: []*discoverd.Instance{node(2, 1), node(1, 2), node(3, 3)},
				},
				Postgres: &simulator.PostgresInfo{
					Online: false,
					Config: &state.PgConfig{
						Role: state.RoleNone,
					},
					XLog: xlog.Zero,
				},
			},
		},
	})
}
