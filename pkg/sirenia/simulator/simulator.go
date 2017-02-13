//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.
//
//
// This file is derived from
// https://github.com/joyent/manatee-state-machine/blob/d441fe941faddb51d6e6237d792dd4d7fae64cc6/lib/sim.js
// https://github.com/joyent/manatee-state-machine/blob/d441fe941faddb51d6e6237d792dd4d7fae64cc6/lib/sim-zk.js
// https://github.com/joyent/manatee-state-machine/blob/d441fe941faddb51d6e6237d792dd4d7fae64cc6/lib/sim-pg.js
//
// Copyright (c) 2014, Joyent, Inc.
// Copyright (c) 2015, Prime Directive, Inc.
//

package simulator

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/flynn/flynn/appliance/postgresql/pgxlog"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/sirenia/state"
	"github.com/flynn/flynn/pkg/sirenia/xlog"
	"gopkg.in/inconshreveable/log15.v2"
)

//TODO(jpg) There isn't really a reason for the simulator to use the postgres xlog
// However all the initial wal harness data is specified using the pg format so
// there is no harm it leaving it for now.
var dxlog = pgxlog.PgXLog{}

const (
	simIdKey = "SIM_ID"
)

type commandFunc func([]string)
type command struct {
	Name     string
	Help     string
	Args     string
	Func     commandFunc
	WaitRest bool
}

const opLag = 1 * time.Millisecond

type Simulator struct {
	singleton bool
	nextPeer  int

	allIdents map[string]*discoverd.Instance

	out       io.Writer
	log       log15.Logger
	db        *databaseSimulator
	discoverd *discoverdSimulator
	peer      *simPeer
	restCh    chan struct{}
	retryCh   chan struct{}
	started   bool

	commands       []*command
	commandsByName map[string]*command
}

func New(singleton bool, out io.Writer, logOut io.Writer) *Simulator {
	if logOut == nil {
		logOut = out
	}
	s := &Simulator{
		singleton: singleton,
		allIdents: make(map[string]*discoverd.Instance),
		out:       out,
		log:       log15.New(),
		restCh:    make(chan struct{}),
		retryCh:   make(chan struct{}, 1),
	}
	s.discoverd = newDiscoverdSimulator(s.log.New("component", "discoverd"), s.restCh)
	s.db = newDatabaseSimulator(s.discoverd, s.log.New("component", "pg"))
	s.peer = s.createSimPeer()
	s.initCommands()
	s.log.SetHandler(log15.StreamHandler(logOut, log15.LogfmtFormat()))
	return s
}

func (s *Simulator) initCommands() {
	s.commands = []*command{
		{"addpeer", "simulate a new peer joining the discoverd cluster", "[NAME]", s.AddPeer, true},
		{"bootstrap", "simulate initial setup", "PRIMARY [SYNC]", s.Bootstrap, true},
		{"catchUp", "simulate peer's postgres catching up to primary", "", s.CatchUp, false},
		{"depose", "simulate a takeover from the current config", "", s.Depose, true},
		{"rebuild", "simulate rebuilding a deposed peer", "NAME", s.Rebuild, true},
		{"echo", "emit the string to stdout", "STR", s.Echo, false},
		{"freeze", "freeze cluster", "", s.Freeze, true},
		{"help", "show help output", "", s.Help, false},
		{"ident", "print the identity of the peer being tested", "", s.Ident, false},
		{"lspeers", "list simulated peers", "", s.LsPeers, false},
		{"peer", "dump peer's current state", "", s.Peer, false},
		{"rmpeer", "simulate a peer being removed from the discoverd cluster", "ID", s.RmPeer, true},
		{"setClusterState", "simulate a write to the cluster state stored in discoverd", "STATE", s.SetClusterState, true},
		{"startPeer", "start the peer state machine", "", s.StartPeer, true},
		{"unfreeze", "unfreeze the cluster", "", s.Unfreeze, true},
		{"discoverd", "print simulated discoverd state", "", s.Discoverd, false},
		{"exit", "exit the simulator", "", s.Exit, false},
	}
	s.commandsByName = make(map[string]*command, len(s.commands))
	for _, c := range s.commands {
		s.commandsByName[strings.ToLower(c.Name)] = c
	}
}

func (s *Simulator) newPeerIdent(name string) *discoverd.Instance {
	s.nextPeer++
	if name == "" {
		name = fmt.Sprintf("node%d", s.nextPeer)
	}
	inst := &discoverd.Instance{
		Addr:  fmt.Sprintf("10.0.0.%d:5432", s.nextPeer),
		Proto: "tcp",
		Meta:  map[string]string{simIdKey: name},
	}
	inst.ID = md5sum(inst.Proto + "-" + inst.Addr)
	s.allIdents[name] = inst
	return inst
}

type simPeer struct {
	Self      *discoverd.Instance
	Peer      *state.Peer
	Discoverd *discoverdSimulatorClient
	Db        *databaseSimulatorClient
}

func (s *Simulator) createSimPeer() *simPeer {
	ident := s.newPeerIdent("")
	dd := s.discoverd.NewClient(ident)
	db := s.db.NewClient(ident)
	p := state.NewPeer(ident, ident.Meta[simIdKey], simIdKey, s.singleton, dd, db, s.log.New("component", "peer"))
	p.SetDebugChannels(s.restCh, s.retryCh)

	return &simPeer{
		Self:      ident,
		Discoverd: dd,
		Db:        db,
		Peer:      p,
	}
}

func (s *Simulator) jsonDump(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "\t")
	fmt.Fprintln(s.out, string(data))
}

func (s *Simulator) Close() error {
	return s.peer.Peer.Close()
}

func (s *Simulator) RunCommand(cmdline string) {
	cmdArgs := strings.SplitN(cmdline, " ", 2)
	name := cmdArgs[0]
	var argStr string
	if len(cmdArgs) > 1 {
		argStr = cmdArgs[1]
	}

	cmd, ok := s.commandsByName[strings.ToLower(name)]
	if !ok {
		s.log.Error("unknown command", "name", name)
		return
	}

	var retryLater bool
	if strings.HasSuffix(argStr, "retrylater") {
		retryLater = true
		argStr = strings.TrimSuffix(argStr, "retrylater")
	}

	var args []string
	if len(cmd.Args) > 0 {
		args = strings.SplitN(strings.TrimSpace(argStr), " ", len(cmd.Args))
	}
	cmd.Func(args)

	if s.started && retryLater {
		select {
		case <-s.retryCh:
		case <-time.After(time.Second):
			panic("timed out waiting for retry signal")
		}
	}
	if s.started && cmd.WaitRest {
		select {
		case <-s.restCh:
		case <-time.After(time.Second):
			panic("timed out waiting for command to run")
		}
	}
}

func (s *Simulator) StartPeer(args []string) {
	s.discoverd.PeerJoined(s.peer.Self)
	go s.peer.Peer.Run()
	s.peer.Discoverd.startSimulation()
	s.peer.Db.startSimulation()
	s.started = true
}

func (s *Simulator) Echo(args []string) {
	if len(args) > 0 {
		fmt.Fprintln(s.out, args[0])
	}
}

func (s *Simulator) Exit(args []string) {
	os.Exit(0)
}

func (s *Simulator) Help(args []string) {
	w := tabwriter.NewWriter(s.out, 0, 8, 0, '\t', 0)
	for _, c := range s.commands {
		fmt.Fprintf(w, "%s\t%s\t%s\n", c.Name, c.Args, c.Help)
	}
	w.Flush()
}

func (s *Simulator) Ident(args []string) {
	fmt.Fprintln(s.out, s.peer.Self.ID, s.peer.Self.Addr)
}

type DiscoverdInfo struct {
	State *state.DiscoverdState `json:"state"`
	Peers []*discoverd.Instance `json:"peers"`
}

func (s *Simulator) Discoverd(args []string) {
	s.jsonDump(DiscoverdInfo{
		State: s.discoverd.ClusterState(),
		Peers: s.discoverd.Peers(),
	})
}

func (s *Simulator) AddPeer(args []string) {
	var name string
	if len(args) > 0 {
		name = args[0]
	}

	peer := s.allIdents[name]
	if peer == nil {
		peer = s.newPeerIdent(name)
	}
	s.discoverd.PeerJoined(peer)

	// If the primary is one of the simulated peers, then simulate the behavior
	// where the primary adds new peers to the async list.
	cs := s.discoverd.ClusterState()
	if cs.State != nil && cs.State.Primary.ID != s.peer.Self.ID &&
		peer.ID != cs.State.Primary.ID &&
		cs.State.Sync != nil && cs.State.Sync.ID != peer.ID {
		cs.State.Async = append(cs.State.Async, peer)
		s.discoverd.SetClusterState(cs, false)
	}

	s.jsonDump(s.discoverd.Peers())
}

func (s *Simulator) RmPeer(args []string) {
	if len(args) != 1 {
		s.log.Error("missing peer name")
		return
	}
	name := args[0]

	if name != s.peer.Self.Meta[simIdKey] {
		s.discoverd.PeerRemoved(name)
	}

	cs := s.discoverd.ClusterState()
	if cs.State != nil && cs.State.Primary.ID != s.peer.Self.ID {
		for i, p := range cs.State.Async {
			if p.Meta[simIdKey] == name {
				cs.State.Async = append(cs.State.Async[:i], cs.State.Async[i+1:]...)
				s.discoverd.SetClusterState(cs, false)
				break
			}
		}
		if cs.State.Sync != nil && cs.State.Sync.Meta[simIdKey] == name && len(cs.State.Async) > 0 {
			cs.State.Generation++
			cs.State.Sync = cs.State.Async[0]
			cs.State.Async = cs.State.Async[1:]
			s.discoverd.SetClusterState(cs, false)
		}
	}
}

func (s *Simulator) Bootstrap(args []string) {
	cs := s.discoverd.ClusterState()
	if cs.State != nil {
		s.log.Error("cluster is already set up")
		return
	}

	peers := s.discoverd.Peers()
	if len(peers) < 2 {
		s.log.Error("setup requires at least two peers")
		return
	}

	var primaryName, syncName string
	if len(args) > 0 {
		primaryName = args[0]
	}
	if len(args) > 1 {
		syncName = args[1]
	}

	cs.State = &state.State{
		Generation: 1,
		InitWAL:    s.peer.Db.XLog().Zero(),
	}
	if primaryName != "" {
		for _, p := range peers {
			switch {
			case p.Meta[simIdKey] == primaryName:
				cs.State.Primary = p
			case syncName != "" && p.Meta[simIdKey] == syncName:
				cs.State.Sync = p
			case syncName == "" && cs.State.Sync == nil:
				cs.State.Sync = p
			default:
				cs.State.Async = append(cs.State.Async, p)
			}
		}
		if cs.State.Primary == nil {
			s.log.Error("requested primary not found", "name", primaryName)
			return
		}
		if cs.State.Sync == nil {
			s.log.Error("requested sync not found", "name", syncName)
			return
		}
	} else {
		cs.State.Primary = peers[0]
		cs.State.Sync = peers[1]
		cs.State.Async = peers[2:]
		fmt.Fprintf(s.out, "selected %q as primary", cs.State.Primary.Meta[simIdKey])
	}

	s.discoverd.SetClusterState(cs, false)
	s.jsonDump(cs)
}

func (s *Simulator) CatchUp(args []string) {
	s.peer.Db.catchUp()
}

func (s *Simulator) Depose(args []string) {
	cs := s.discoverd.ClusterState()
	if cs.State == nil {
		s.log.Error("cluster is not yet configured (try `bootstrap`)")
		return
	}
	if len(cs.State.Async) == 0 {
		s.log.Error("cannot depose with no asyncs")
		return
	}

	newWAL, err := dxlog.Increment(cs.State.InitWAL, 10)
	if err != nil {
		panic(err)
	}
	cs.State = &state.State{
		Generation: cs.State.Generation + 1,
		Deposed:    append(cs.State.Deposed, cs.State.Primary),
		Primary:    cs.State.Sync,
		Sync:       cs.State.Async[0],
		Async:      cs.State.Async[1:],
		InitWAL:    newWAL,
	}
	s.discoverd.SetClusterState(cs, false)
	s.jsonDump(cs)
}

func (s *Simulator) Rebuild(args []string) {
	if len(args) == 0 {
		s.log.Error("missing peer name argument")
		return
	}
	name := args[0]

	cs := s.discoverd.ClusterState()
	if cs.State == nil {
		s.log.Error("cluster is not yet configured (try `bootstrap`)")
		return
	}
	var peer *discoverd.Instance

	cs.State = cs.State.Clone()
	newDeposed := make([]*discoverd.Instance, 0, len(cs.State.Deposed))
	for _, p := range cs.State.Deposed {
		if p.Meta[simIdKey] == name {
			peer = p
			continue
		}
		newDeposed = append(newDeposed, p)
	}
	cs.State.Deposed = newDeposed
	if peer == nil {
		s.log.Error("peer is not deposed", "name", name)
		return
	}

	// If we are simulating the current primary, make the newly rebuilt peer an
	// async now.
	if cs.State.Primary.ID != s.peer.Self.ID {
		cs.State.Async = append(cs.State.Async, peer)
	}

	s.discoverd.SetClusterState(cs, false)
	s.jsonDump(cs)
}

func (s *Simulator) Freeze(args []string) {
	cs := s.discoverd.ClusterState()
	cs.State.Freeze = &state.FreezeDetails{
		FrozenAt: state.TimeNow(),
		Reason:   "frozen by simulator",
	}
	s.discoverd.SetClusterState(cs, false)
	s.jsonDump(cs)
}

func (s *Simulator) Unfreeze(args []string) {
	cs := s.discoverd.ClusterState()
	cs.State.Freeze = nil
	s.discoverd.SetClusterState(cs, false)
	s.jsonDump(cs)
}

func (s *Simulator) LsPeers(args []string) {
	s.jsonDump(s.discoverd.Peers())
}

type PeerSimInfo struct {
	Peer *state.PeerInfo `json:"peer"`
	Db   *DbInfo         `json:"postgres"`
}

func (s *Simulator) Peer(args []string) {
	s.jsonDump(PeerSimInfo{
		Peer: s.peer.Peer.Info(),
		Db:   s.peer.Db.Info(),
	})
}

func (s *Simulator) SetClusterState(args []string) {
	if len(args) == 0 {
		s.log.Error("missing state")
		return
	}
	cs := s.discoverd.ClusterState()
	if err := json.Unmarshal([]byte(args[0]), &cs.State); err != nil {
		s.log.Error("error decoding state", "err", err)
		return
	}
	s.discoverd.SetClusterState(cs, false)
}

func md5sum(data string) string {
	digest := md5.Sum([]byte(data))
	return hex.EncodeToString(digest[:])
}

type discoverdSimulator struct {
	sync.Mutex
	log     log15.Logger
	state   *state.DiscoverdState
	peers   []*discoverd.Instance
	clients []*discoverdSimulatorClient
	restCh  chan struct{}
}

func newDiscoverdSimulator(log log15.Logger, restCh chan struct{}) *discoverdSimulator {
	return &discoverdSimulator{
		log:    log,
		state:  &state.DiscoverdState{},
		restCh: restCh,
	}
}

func (d *discoverdSimulator) NewClient(inst *discoverd.Instance) *discoverdSimulatorClient {
	c := &discoverdSimulatorClient{
		d:      d,
		inst:   inst,
		events: make(chan *state.DiscoverdEvent),
	}
	d.clients = append(d.clients, c)
	return c
}

func (d *discoverdSimulator) PeerJoined(inst *discoverd.Instance) {
	d.Lock()
	defer d.Unlock()

	for _, p := range d.peers {
		if p.ID == inst.ID {
			d.log.Error("peer already exists", "addr", inst.Addr)
			return
		}
	}

	inst.Index = 1
	if len(d.peers) > 0 {
		inst.Index = d.peers[len(d.peers)-1].Index + 1
	}
	inst = inst.Clone()
	d.peers = append(d.peers, inst)
	for _, c := range d.clients {
		c.notifyPeersChanged()
	}
}

func (d *discoverdSimulator) PeerRemoved(name string) {
	d.Lock()
	defer d.Unlock()

	removed := false
	for i, p := range d.peers {
		if p.Meta[simIdKey] == name {
			d.peers = append(d.peers[:i], d.peers[i+1:]...)
			removed = true
			break
		}
	}
	if !removed {
		d.log.Error("peer not present", "name", name)
		return
	}
	for _, c := range d.clients {
		c.notifyPeersChanged()
	}
}

func (d *discoverdSimulator) SetClusterState(s *state.DiscoverdState, async bool) {
	d.Lock()
	defer d.Unlock()

	if d.state.Index != s.Index {
		panic(fmt.Sprintf("incorrect state index, have %d, want %d", s.Index, d.state.Index))
	}
	d.state.Index++
	d.state.State = s.State.Clone()
	s.Index = d.state.Index
	for _, c := range d.clients {
		if async {
			go c.notifyStateChanged(d._clusterState())
		} else {
			c.notifyStateChanged(d._clusterState())
		}
	}
}

func (d *discoverdSimulator) ClusterState() *state.DiscoverdState {
	d.Lock()
	defer d.Unlock()
	return d._clusterState()
}

func (d *discoverdSimulator) _clusterState() *state.DiscoverdState {
	return &state.DiscoverdState{
		Index: d.state.Index,
		State: d.state.State.Clone(),
	}
}

func (d *discoverdSimulator) Peers() []*discoverd.Instance {
	d.Lock()
	defer d.Unlock()
	return d._peers()
}

func (d *discoverdSimulator) _peers() []*discoverd.Instance {
	res := make([]*discoverd.Instance, len(d.peers))
	copy(res, d.peers)
	return res
}

type discoverdSimulatorClient struct {
	sync.RWMutex
	d      *discoverdSimulator
	inst   *discoverd.Instance
	events chan *state.DiscoverdEvent
	init   bool
}

func (d *discoverdSimulatorClient) SetState(s *state.DiscoverdState) error {
	time.Sleep(opLag)
	d.d.SetClusterState(s, true)
	return nil
}

func (d *discoverdSimulatorClient) Events() <-chan *state.DiscoverdEvent {
	return d.events
}

func (d *discoverdSimulatorClient) notifyPeersChanged() {
	d.RLock()
	defer d.RUnlock()

	if !d.init {
		return
	}
	for {
		select {
		case d.events <- &state.DiscoverdEvent{
			Kind:  state.DiscoverdEventPeers,
			Peers: d.d._peers(),
		}:
			return
		case <-d.d.restCh:
			continue
		}
	}
}

func (d *discoverdSimulatorClient) notifyStateChanged(s *state.DiscoverdState) {
	d.RLock()
	defer d.RUnlock()

	if !d.init {
		return
	}
	for {
		select {
		case d.events <- &state.DiscoverdEvent{
			Kind:  state.DiscoverdEventState,
			State: s,
		}:
			return
		case <-d.d.restCh:
			continue
		}
	}
}

func (d *discoverdSimulatorClient) startSimulation() {
	d.events <- &state.DiscoverdEvent{
		Kind:  state.DiscoverdEventInit,
		State: d.d.ClusterState(),
		Peers: d.d.Peers(),
	}

	d.Lock()
	d.init = true
	d.Unlock()
}

type databaseSimulator struct {
	log log15.Logger
	ds  *discoverdSimulator
}

func newDatabaseSimulator(ds *discoverdSimulator, log log15.Logger) *databaseSimulator {
	return &databaseSimulator{ds: ds, log: log}
}

func (p *databaseSimulator) NewClient(inst *discoverd.Instance) *databaseSimulatorClient {
	c := &databaseSimulatorClient{
		p:      p,
		inst:   inst,
		events: make(chan state.DatabaseEvent),
	}
	c.CurXLog = c.XLog().Zero()
	return c
}

type databaseSimulatorClient struct {
	p      *databaseSimulator
	inst   *discoverd.Instance
	events chan state.DatabaseEvent

	DbInfo
}

type DbInfo struct {
	Config      *state.Config `json:"config"`
	Online      bool          `json:"online"`
	CurXLog     xlog.Position `json:"xlog"`
	XLogWaiting xlog.Position `json:"xlog_waiting,omitempty"`
}

func (d *DbInfo) XLog() xlog.XLog {
	return dxlog
}

func (p *databaseSimulatorClient) Info() *DbInfo {
	return &p.DbInfo
}

func (p *databaseSimulatorClient) startSimulation() {
	p.events <- state.DatabaseEvent{}
}

func (p *databaseSimulatorClient) XLogPosition() (xlog.Position, error) {
	time.Sleep(opLag)
	if !p.Online {
		return "", fmt.Errorf("database is offline")
	}
	return p.CurXLog, nil
}

func (p *databaseSimulatorClient) Reconfigure(conf *state.Config) error {
	s := p.p.ds.ClusterState()
	if s.State == nil && conf.Role != state.RoleNone {
		panic("attempted to configure database with no cluster state")
	}

	if p.Config.Equal(conf) {
		return nil // nothing to apply, Upstream/Downstreams match - just state update
	}

	p.XLogWaiting = ""
	p.p.log.Info("reconfiguring database")
	time.Sleep(opLag)
	p.Config = conf
	p.updateXlog(s)

	return nil
}

func (p *databaseSimulatorClient) Start() error {
	if p.Config == nil {
		panic("cannot call Start before configured")
	}
	if p.Online {
		panic("cannot call Start while running")
	}
	if p.XLogWaiting != "" {
		panic(fmt.Sprintf("unexpected xlog_waiting %q", p.XLogWaiting))
	}

	p.p.log.Info("starting database")
	time.Sleep(opLag)
	p.Online = true
	p.updateXlog(p.p.ds.ClusterState())

	return nil
}

func (p *databaseSimulatorClient) Stop() error {
	if !p.Online {
		panic("cannot call Stop while stopped")
	}

	p.p.log.Info("stopping database")
	time.Sleep(opLag)
	p.Online = false

	return nil
}

func (p *databaseSimulatorClient) Ready() <-chan state.DatabaseEvent {
	return p.events
}

func (p *databaseSimulatorClient) DefaultTunables() state.Tunables {
	return state.Tunables{
		Version: 0,
	}
}

func (p *databaseSimulatorClient) ValidateTunables(_ state.Tunables) error {
	return nil
}

// Given the current state, figure out our current role and update our xlog
// position accordingly. This is used when we assume a new role or when database
// comes online in order to simulate client writes to the primary, synchronous
// replication (and catch-up) on the sync, and asynchronous replication on the
// other peers.
func (p *databaseSimulatorClient) updateXlog(ds *state.DiscoverdState) {
	if ds.State == nil || !p.Online || p.Config == nil {
		return
	}
	s := ds.State

	var role state.Role
	switch {
	case s.Primary.ID == p.inst.ID:
		role = state.RolePrimary
	case s.Sync.ID == p.inst.ID:
		role = state.RoleSync
	case p.Config.Role == state.RoleAsync:
		role = state.RoleAsync
	default:
		role = state.RoleNone
	}

	// If the peer we're testing is an async or unassigned, we don't modify the
	// transaction log position at all. We act as though these are getting
	// arbitrarily far behind (since that should be fine).
	if role == state.RoleAsync || role == state.RoleNone {
		return
	}

	// If the peer we're testing is a primary, we act as though the sync
	// instantly connected and caught up, and we start taking writes immediately
	// and bump the transaction log position.
	if role == state.RolePrimary {
		if cmp, err := dxlog.Compare(s.InitWAL, p.CurXLog); err != nil {
			panic(err)
		} else if cmp > 0 {
			panic("primary is behind the generation's initial xlog")
		}
		var err error
		p.CurXLog, err = dxlog.Increment(p.CurXLog, 10)
		if err != nil {
			panic(err)
		}
		return
	}

	// The most complicated case is the sync, for which we need to schedule the
	// wal position to catch up to the primary's.
	if role != state.RoleSync {
		panic("unexpected role")
	}
	if cmp, err := dxlog.Compare(s.InitWAL, p.CurXLog); err != nil {
		panic(err)
	} else if cmp < 0 {
		panic("sync is ahead of primary")
	}
	p.XLogWaiting = s.InitWAL
}

func (p *databaseSimulatorClient) catchUp() {
	if p.XLogWaiting == "" {
		p.p.log.Error("catchUp when not sync or not currently waiting")
	}
	var err error
	p.CurXLog, err = dxlog.Increment(p.XLogWaiting, 10)
	if err != nil {
		panic(err)
	}
	p.XLogWaiting = ""
}
