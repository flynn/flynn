//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.
//
//
// This file is derived from
// https://github.com/joyent/manatee-state-machine/blob/d441fe941faddb51d6e6237d792dd4d7fae64cc6/lib/manatee-peer.js
//
// Copyright (c) 2014, Joyent, Inc.
// Copyright (c) 2015, Prime Directive, Inc.
//

package state

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/sirenia/xlog"
)

type State struct {
	Generation int                   `json:"generation"`
	Freeze     *FreezeDetails        `json:"freeze,omitempty"`
	Singleton  bool                  `json:"singleton,omitempty"`
	InitWAL    xlog.Position         `json:"init_wal"`
	Primary    *discoverd.Instance   `json:"primary"`
	Sync       *discoverd.Instance   `json:"sync"`
	Async      []*discoverd.Instance `json:"async"`
	Deposed    []*discoverd.Instance `json:"deposed,omitempty"`
}

func (s *State) Clone() *State {
	if s == nil {
		return nil
	}
	res := *s
	if s.Freeze != nil {
		f := *s.Freeze
		res.Freeze = &f
	}
	if s.Async != nil {
		res.Async = make([]*discoverd.Instance, len(s.Async))
		copy(res.Async, s.Async)
	}
	if s.Deposed != nil {
		res.Deposed = make([]*discoverd.Instance, len(s.Deposed))
		copy(res.Deposed, s.Deposed)
	}
	return &res
}

type FreezeDetails struct {
	FrozenAt time.Time `json:"frozen_at"`
	Reason   string    `json:"reason"`
}

var TimeNow = func() time.Time {
	return time.Now().UTC()
}

func NewFreezeDetails(reason string) *FreezeDetails {
	return &FreezeDetails{
		FrozenAt: TimeNow(),
		Reason:   reason,
	}
}

type Role int

const (
	RoleUnknown Role = iota
	RolePrimary
	RoleSync
	RoleAsync
	RoleUnassigned
	RoleDeposed
	RoleNone
)

var roleStrings = map[Role]string{
	RoleUnknown:    "unknown",
	RolePrimary:    "primary",
	RoleSync:       "sync",
	RoleAsync:      "async",
	RoleUnassigned: "unassigned",
	RoleDeposed:    "deposed",
	RoleNone:       "none",
}

var roleJSON = make(map[string]Role, len(roleStrings))

func init() {
	for k, v := range roleStrings {
		roleJSON[`"`+v+`"`] = k
	}
}

func (r Role) String() string {
	return roleStrings[r]
}

func (r Role) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, roleStrings[r])), nil
}

func (r *Role) UnmarshalJSON(b []byte) error {
	*r = roleJSON[string(b)]
	return nil
}

type Config struct {
	Role       Role                `json:"role"`
	Upstream   *discoverd.Instance `json:"upstream"`
	Downstream *discoverd.Instance `json:"downstream"`
}

func peersEqual(a, b *discoverd.Instance) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ID == b.ID

}

func (x *Config) Equal(y *Config) bool {
	if x == nil || y == nil {
		return x == y
	}

	return x.Role == y.Role && peersEqual(x.Upstream, y.Upstream) && peersEqual(x.Downstream, y.Downstream)
}

func (x *Config) IsNewDownstream(y *Config) bool {
	if x == nil || y == nil {
		return false
	}
	if !peersEqual(x.Upstream, y.Upstream) {
		return false
	}
	return y.Downstream != nil && !peersEqual(x.Downstream, y.Downstream)
}

type Database interface {
	// TODO(jpg): need a more generic representation of database log position.
	// the only important thing here is to ensure we have an ordering.
	// the comparison of this should also be handed of to the underlying database wrapper
	XLogPosition() (xlog.Position, error)
	// Returns the implementation of the XLog interface for this database
	XLog() xlog.XLog
	Reconfigure(*Config) error
	Start() error
	Stop() error

	// Ready returns a channel that returns a single event when the interface
	// is ready.
	Ready() <-chan DatabaseEvent
}

type Discoverd interface {
	SetState(*DiscoverdState) error
	Events() <-chan *DiscoverdEvent
}

type DatabaseEvent struct {
	Online bool
	Setup  bool
}

type DiscoverdEventKind int

const (
	DiscoverdEventInit DiscoverdEventKind = iota
	DiscoverdEventState
	DiscoverdEventPeers
)

type DiscoverdState struct {
	Index uint64 `json:"index"`
	State *State `json:"state"`
}

type DiscoverdEvent struct {
	Kind  DiscoverdEventKind
	Peers []*discoverd.Instance
	State *DiscoverdState
}

type PeerInfo struct {
	ID           string                `json:"id"`
	Role         Role                  `json:"role"`
	RetryPending *time.Time            `json:"retry_pending,omitempty"`
	State        *State                `json:"state"`
	Peers        []*discoverd.Instance `json:"peers"`
}

type Peer struct {
	// Configuration
	id        string
	idKey     string
	self      *discoverd.Instance
	singleton bool

	// External Interfaces
	log       log15.Logger
	discoverd Discoverd
	db        Database

	// Dynamic state
	info          atomic.Value // *PeerInfo, replaced after each change
	generation    int          // last generation updated processed
	updatingState *State       // new state object
	stateIndex    uint64       // last received cluster state index

	online     *bool               // nil for unknown
	setup      bool                // whether db existed at start
	applied    *Config             // last configuration applied
	upstream   *discoverd.Instance // upstream replication target
	downstream *discoverd.Instance // downstream replication target

	evalStateCh chan struct{}
	applyConfCh chan struct{}
	restCh      chan struct{}
	workDoneCh  chan struct{}
	retryCh     chan struct{}
	stopCh      chan struct{}

	closeOnce sync.Once
}

func NewPeer(self *discoverd.Instance, id string, idKey string, singleton bool, d Discoverd, db Database, log log15.Logger) *Peer {
	p := &Peer{
		id:          id,
		idKey:       idKey,
		self:        self,
		singleton:   singleton,
		db:          db,
		discoverd:   d,
		log:         log,
		evalStateCh: make(chan struct{}, 1),
		applyConfCh: make(chan struct{}, 1),
		stopCh:      make(chan struct{}),
	}
	p.info.Store(&PeerInfo{ID: id})
	return p
}

func (p *Peer) SetDebugChannels(restCh, retryCh chan struct{}) {
	p.restCh = restCh
	p.retryCh = retryCh
}

func (p *Peer) Run() {
	discoverdCh := p.discoverd.Events()
	dbCh := p.db.Ready()
	for {
		select {
		// drain discoverdCh to avoid evaluating out-of-date state
		case e := <-discoverdCh:
			p.handleDiscoverdEvent(e)
			continue
		case <-p.stopCh:
			return
		default:
		}
		select {
		case e := <-discoverdCh:
			p.handleDiscoverdEvent(e)
			continue
		case e := <-dbCh:
			p.handleInit(e)
			continue
		case <-p.evalStateCh:
			p.evalClusterState()
			continue
		case <-p.applyConfCh:
			p.applyConfig()
			continue
		case <-p.stopCh:
			return
		default:
			// There were no receivable channels, fallthrough to the select that
			// includes the work done channel
		}
		select {
		case e := <-discoverdCh:
			p.handleDiscoverdEvent(e)
		case e := <-dbCh:
			p.handleInit(e)
		case <-p.evalStateCh:
			p.evalClusterState()
		case <-p.applyConfCh:
			p.applyConfig()
		case <-p.workDoneCh:
			// There is no work to do, we are now at rest
			p.rest()
		case <-p.stopCh:
			return
		}
	}
}

func (p *Peer) Stop() error {
	p.Close()
	return p.db.Stop()
}

func (p *Peer) Close() error {
	p.closeOnce.Do(func() {
		close(p.stopCh)
	})
	return nil
}

func (p *Peer) Info() *PeerInfo {
	return p.info.Load().(*PeerInfo)
}

func (p *Peer) setInfo(info PeerInfo) {
	p.info.Store(&info)
}

func (p *Peer) setState(state *State) {
	info := *p.Info()
	info.State = state
	p.setInfo(info)
}

func (p *Peer) setPeers(peers []*discoverd.Instance) {
	info := *p.Info()
	info.Peers = peers
	p.setInfo(info)
}

func (p *Peer) setRole(role Role) {
	info := *p.Info()
	info.Role = role
	p.setInfo(info)
}

func (p *Peer) setRetryPending(t *time.Time) {
	info := *p.Info()
	info.RetryPending = t
	p.setInfo(info)
}

func (p *Peer) handleInit(e DatabaseEvent) {
	p.log.Info("db init", "online", e.Online, "setup", e.Setup)
	if p.online != nil {
		panic("received db init event after already initialized")
	}

	p.online = &e.Online
	p.setup = e.Setup

	if p.Info().Peers != nil {
		p.evalClusterState()
	}
}

func (p *Peer) handleDiscoverdEvent(e *DiscoverdEvent) {
	switch e.Kind {
	case DiscoverdEventInit:
		p.handleDiscoverdInit(e)
	case DiscoverdEventState:
		p.handleDiscoverdState(e)
	case DiscoverdEventPeers:
		p.handleDiscoverdPeers(e)
	}
}

func (p *Peer) handleDiscoverdInit(e *DiscoverdEvent) {
	p.log.Info("discoverd init", "peers", len(e.Peers), "state", e.State.State != nil)
	if p.Info().State != nil {
		panic("received discoverd init after already initialized")
	}
	p.setPeers(e.Peers)
	p.decodeState(e)
	if p.online != nil {
		p.triggerEval()
	}
}

func (p *Peer) handleDiscoverdPeers(e *DiscoverdEvent) {
	p.log.Info("discoverd peers", "peers", len(e.Peers))
	info := *p.Info()
	if info.Peers == nil {
		panic("received discoverd peers before init")
	}
	p.setPeers(e.Peers)
	p.triggerEval()
}

func (p *Peer) handleDiscoverdState(e *DiscoverdEvent) {
	log := p.log.New("fn", "handleDiscoverdState", "index", e.State.Index)
	log.Info("got discoverd state", "state", e.State.State != nil)
	if p.Info().Peers == nil {
		panic("received discoverd state before init")
	}
	if p.decodeState(e) {
		p.triggerEval()
	} else {
		log.Info("already have this state")
	}
}

func (p *Peer) decodeState(e *DiscoverdEvent) bool {
	if e.State.Index > p.stateIndex {
		p.stateIndex = e.State.Index
		p.setState(e.State.State)
		return true
	}
	return false
}

// evalLater triggers a cluster state evaluation after delay has elapsed
func (p *Peer) evalLater(delay time.Duration) {
	if p.retryCh != nil {
		p.retryCh <- struct{}{}
		return
	}
	time.AfterFunc(delay, p.triggerEval)
}

func (p *Peer) applyConfigLater(delay time.Duration) {
	if p.retryCh != nil {
		p.retryCh <- struct{}{}
		return
	}
	time.AfterFunc(delay, p.triggerApplyConfig)
}

func (p *Peer) triggerEval() {
	select {
	case p.evalStateCh <- struct{}{}:
	default:
		// if we can't send to the channel, there is already a pending state
		// evaluation, as it is buffered
	}
}

func (p *Peer) triggerApplyConfig() {
	select {
	case p.applyConfCh <- struct{}{}:
	default:
		// if we can't send to the channel, there is already a pending
		// configuration, as it is buffered
	}
}

func (p *Peer) moving() {
	if !p.atRest() {
		return
	}
	p.log.Debug("moving")

	// Set up a channel that the select in the run loop can use to detect
	// when there is no more work to do.
	p.workDoneCh = make(chan struct{}, 1)
	p.workDoneCh <- struct{}{}
}

func (p *Peer) rest() {
	p.log.Debug("at rest", "fn", "rest")
	p.workDoneCh = nil
	if p.restCh != nil {
		p.restCh <- struct{}{}
	}
}

func (p *Peer) atRest() bool {
	return p.workDoneCh == nil
}

// Examine the current cluster state and determine if new actions need to be
// taken. For example, if we're the primary, and there's no sync present, then
// we need to declare a new generation.
func (p *Peer) evalClusterState() {
	p.moving()
	log := p.log.New("fn", "evalClusterState")
	log.Info("starting state evaluation")

	// If there's no cluster state, check whether we should set up the cluster.
	// If not, wait for something else to happen.
	if info := p.Info(); info.State == nil {
		log.Debug("no cluster state",
			"peers", len(info.Peers),
			"self", p.id,
			"singleton", p.singleton,
			"leader", len(info.Peers) > 0 && info.Peers[0].Meta[p.idKey] == p.id,
		)

		if len(info.Peers) == 0 {
			return
		}

		if !p.setup &&
			info.Peers[0].Meta[p.idKey] == p.id &&
			(p.singleton || len(info.Peers) > 1) {
			p.startInitialSetup()
		} else if info.Role != RoleUnassigned {
			p.assumeUnassigned()
		}

		return
	}

	// Bail out if we're configured for singleton mode but the cluster is not.
	if p.singleton && !p.Info().State.Singleton {
		panic("configured for singleton mode but cluster found in normal mode")
	}

	// If the generation has changed, then go back to square one (unless we
	// think we're the primary but no longer are, in which case it's game over).
	// This may cause us to update our role and then trigger another call to
	// evalClusterState() to deal with any other changes required. We update
	// p.generation so that we know that we've handled the generation change
	// already.
	if p.generation != p.Info().State.Generation {
		p.generation = p.Info().State.Generation

		if p.Info().Role == RolePrimary {
			if p.Info().State.Primary.Meta[p.idKey] != p.id {
				p.assumeDeposed()
			}
		} else {
			p.evalInitClusterState()
		}

		return
	}

	// Unassigned peers and async peers only need to watch their position in the
	// async peer list and reconfigure themselves as needed
	if p.Info().Role == RoleUnassigned {
		if i := p.whichAsync(); i != -1 {
			p.assumeAsync(i)
		}
		return
	}

	if p.Info().Role == RoleAsync {
		if whichAsync := p.whichAsync(); whichAsync == -1 {
			p.assumeUnassigned()
		} else {
			upstream := p.lookupUpstream(whichAsync)
			downstream := p.lookupDownstream(whichAsync)
			if upstream.Meta[p.idKey] != p.upstream.Meta[p.idKey] ||
				downstream != nil && (p.downstream == nil || downstream.Meta[p.idKey] != p.downstream.Meta[p.idKey]) {
				p.assumeAsync(whichAsync)
			}
		}
		return
	}

	// The synchronous peer needs to check the takeover condition, which is that
	// the primary has disappeared and the sync's WAL has caught up enough to
	// takeover as primary.
	if p.Info().Role == RoleSync {
		if !p.peerIsPresent(p.Info().State.Primary) {
			p.startTakeover("primary gone", p.Info().State.InitWAL)
		} else if len(p.Info().State.Async) > 0 && (p.downstream == nil || p.downstream.Meta[p.idKey] != p.Info().State.Async[0].Meta[p.idKey]) {
			p.assumeSync()
		}
		return
	}

	if p.Info().Role != RolePrimary {
		panic(fmt.Sprintf("unexpected role %v", p.Info().Role))
	}

	// write new state with updated discoverd instance ID if our discoverd
	// instance has changed (new job with the same local instance ID saved in
	// the data volume)
	if p.Info().State.Primary.ID != p.self.ID && p.Info().State.Primary.Meta[p.idKey] == p.id {
		log.Info("role is primary, but discoverd id in state differs from self, updating")
		p.updatingState = p.Info().State
		p.updatingState.Primary = p.self
		if err := p.putClusterState(); err != nil {
			log.Error("failed to update cluster state", "err", err)
		} else {
			p.setState(p.updatingState)
		}
		p.updatingState = nil
	}

	if p.Info().State.Freeze != nil {
		log.Info("cluster frozen, not making any changes")
		return
	}

	if !p.singleton && p.Info().State.Singleton {
		log.Info("configured for normal mode but found cluster in singleton mode, transitioning cluster to normal mode")
		if p.Info().State.Primary.Meta[p.idKey] != p.id {
			panic(fmt.Sprintf("unexpected cluster state, we should be the primary, but %s is", p.Info().State.Primary.Meta[p.idKey]))
		}
		p.startTransitionToNormalMode()
		return
	}

	// The primary peer needs to check not just for liveness of the synchronous
	// peer, but also for other new or removed peers. We only do this in normal
	// mode, not one-node-write mode.
	if p.singleton {
		return
	}

	// TODO: It would be nice to process the async peers showing up and
	// disappearing as part of the same cluster state change update as our
	// takeover attempt. As long as we're not, though, we must handle the case
	// that we go to start a takeover, but we cannot proceed because there are
	// no asyncs. In that case, we want to go ahead and process the asyncs, then
	// consider a takeover the next time around. If we update this to handle
	// both operations at once, we can get rid of the goofy boolean returned by
	// startTakeover.
	if !p.peerIsPresent(p.Info().State.Sync) &&
		p.startTakeover("sync gone", p.Info().State.InitWAL) {
		return
	}

	presentPeers := make(map[string]struct{}, len(p.Info().Peers))
	presentPeers[p.Info().State.Primary.Meta[p.idKey]] = struct{}{}
	presentPeers[p.Info().State.Sync.Meta[p.idKey]] = struct{}{}

	newAsync := make([]*discoverd.Instance, 0, len(p.Info().Peers))
	changes := false

	for _, a := range p.Info().State.Async {
		if p.peerIsPresent(a) {
			presentPeers[a.Meta[p.idKey]] = struct{}{}
			newAsync = append(newAsync, a)
		} else {
			log.Debug("peer missing", "async.id", a.Meta[p.idKey], "async.addr", a.Addr)
			changes = true
		}
	}

	// Deposed peers should not be assigned as asyncs
	for _, d := range p.Info().State.Deposed {
		presentPeers[d.Meta[p.idKey]] = struct{}{}
	}

	for _, peer := range p.Info().Peers {
		if _, ok := presentPeers[peer.Meta[p.idKey]]; ok {
			continue
		}
		log.Debug("new peer", "async.id", peer.Meta[p.idKey], "async.addr", peer.Addr)
		newAsync = append(newAsync, peer)
		changes = true
	}

	if !changes {
		return
	}

	p.startUpdateAsyncs(newAsync)
}

func (p *Peer) startInitialSetup() {
	if p.updatingState != nil {
		panic("already have updating state")
	}

	p.updatingState = &State{
		Generation: 1,
		Primary:    p.self,
		InitWAL:    p.db.XLog().Zero(),
	}
	if p.singleton {
		p.updatingState.Singleton = true
		p.updatingState.Freeze = NewFreezeDetails("cluster started in singleton mode")
	} else {
		info := p.Info()
		p.updatingState.Sync = info.Peers[1]
		p.updatingState.Async = info.Peers[2:]
	}
	log := p.log.New("fn", "startInitialSetup")
	log.Info("creating initial cluster state", "generation", 1)

	if err := p.putClusterState(); err != nil {
		log.Error("failed to create cluster state", "err", err)
		p.evalLater(1 * time.Second)
		return
	} else {
		info := *p.Info()
		info.State = p.updatingState
		p.setInfo(info)
	}
	p.updatingState = nil

	p.triggerEval()
}

func (p *Peer) assumeUnassigned() {
	p.log.Info("assuming unassigned role", "role", "unassigned", "fn", "assumeUnassigned")
	p.setRole(RoleUnassigned)
	p.upstream = nil
	p.downstream = nil
	p.triggerApplyConfig()
}

func (p *Peer) assumeDeposed() {
	p.log.Info("assuming deposed role", "role", "deposed", "fn", "assumeDeposed")
	p.setRole(RoleDeposed)
	p.upstream = nil
	p.downstream = nil
	p.triggerApplyConfig()
}

func (p *Peer) assumePrimary() {
	p.log.Info("assuming primary role", "role", "primary", "fn", "assumePrimary")
	p.setRole(RolePrimary)
	p.upstream = nil
	p.downstream = p.Info().State.Sync

	// It simplifies things to say that evalClusterState() only deals with one
	// change at a time. Now that we've handled the change to become primary,
	// check for other changes.
	//
	// For example, we may have just read the initial state that identifies us
	// as the primary, and we may also discover that the synchronous peer is
	// not present. The first call to evalClusterState() will get us here, and
	// we call it again to check for the presence of the synchronous peer.
	//
	// We invoke applyConfig() after evalClusterState(), though it may well
	// turn out that evalClusterState() kicked off an operation that will
	// change the desired postgres configuration. In that case, we'll end up
	// calling applyConfig() again.
	p.evalClusterState()
	p.triggerApplyConfig()
}

func (p *Peer) assumeSync() {
	if p.singleton {
		panic("assumeSync as singleton")
	}
	p.log.Info("assuming sync role", "role", "sync", "fn", "assumeSync")

	p.setRole(RoleSync)
	p.upstream = p.Info().State.Primary
	if len(p.Info().State.Async) > 0 {
		p.downstream = p.Info().State.Async[0]
	}
	// See assumePrimary()
	p.evalClusterState()
	p.triggerApplyConfig()
}

func (p *Peer) assumeAsync(i int) {
	if p.singleton {
		panic("assumeAsync as singleton")
	}
	p.log.Info("assuming async role", "role", "async", "fn", "assumeAsync")

	p.setRole(RoleAsync)
	p.upstream = p.lookupUpstream(i)
	p.downstream = p.lookupDownstream(i)

	// See assumePrimary(). We don't need to check the cluster state here
	// because there's never more than one thing to do when becoming the async
	// peer.
	p.triggerApplyConfig()
}

func (p *Peer) evalInitClusterState() {
	if p.Info().State.Primary.Meta[p.idKey] == p.id {
		p.assumePrimary()
		return
	}
	if p.Info().State.Singleton {
		p.assumeUnassigned()
		return
	}
	if p.Info().State.Sync.Meta[p.idKey] == p.id {
		p.assumeSync()
		return
	}

	for _, d := range p.Info().State.Deposed {
		if p.id == d.Meta[p.idKey] {
			p.assumeDeposed()
			return
		}
	}

	// If we're an async, figure out which one we are.
	if i := p.whichAsync(); i != -1 {
		p.assumeAsync(i)
		return
	}

	p.assumeUnassigned()
}

func (p *Peer) startTakeover(reason string, minWAL xlog.Position) bool {
	log := p.log.New("fn", "startTakeover", "reason", reason, "min_wal", minWAL)

	// Select the first present async peer to be the next sync
	var newSync *discoverd.Instance
	for _, a := range p.Info().State.Async {
		if p.peerIsPresent(a) {
			newSync = a
			break
		}
	}
	if newSync == nil {
		log.Warn("would takeover but no async peers present")
		return false
	}

	log.Debug("preparing for new generation")
	newAsync := make([]*discoverd.Instance, 0, len(p.Info().State.Async))
	for _, a := range p.Info().State.Async {
		if a.Meta[p.idKey] != newSync.Meta[p.idKey] && p.peerIsPresent(a) {
			newAsync = append(newAsync, a)
		}
	}

	newDeposed := append(make([]*discoverd.Instance, 0, len(p.Info().State.Deposed)+1), p.Info().State.Deposed...)
	if p.Info().State.Primary.Meta[p.idKey] != p.id {
		newDeposed = append(newDeposed, p.Info().State.Primary)
	}

	p.startTakeoverWithPeer(reason, minWAL, &State{
		Sync:    newSync,
		Async:   newAsync,
		Deposed: newDeposed,
	})
	return true
}

var (
	ErrClusterFrozen   = errors.New("cluster is frozen")
	ErrDatabaseOffline = errors.New("database is offline")
	ErrPeerNotCaughtUp = errors.New("peer is not caught up")
)

func (p *Peer) startTakeoverWithPeer(reason string, minWAL xlog.Position, newState *State) (err error) {
	log := p.log.New("fn", "startTakeoverWithPeer", "reason", reason, "min_wal", minWAL)
	log.Info("starting takeover")

	if p.updatingState != nil {
		panic("startTakeoverWithPeer with non-nil updatingState")
	}
	newState.Generation = p.Info().State.Generation + 1
	newState.Primary = p.self
	p.updatingState = newState

	if p.updatingState.Primary.Meta[p.idKey] != p.Info().State.Primary.Meta[p.idKey] && len(p.updatingState.Deposed) == 0 {
		panic("startTakeoverWithPeer without deposing old primary")
	}

	defer func() {
		if err == nil {
			return
		}
		p.updatingState = nil

		switch err {
		case ErrDatabaseOffline:
			// If database is offline, it's because we haven't started yet, so
			// trigger another state evaluation after we start it.
			log.Error("failed to declare new generation, trying later", "err", err)
			p.triggerEval()
		case ErrClusterFrozen:
			log.Error("failed to declare new generation", "err", err)
		default:
			// In the event of an error, back off a bit and check state again in
			// a second. There are several transient failure modes that will resolve
			// themselves (e.g. synchronous replication not yet caught up).
			log.Error("failed to declare new generation, backing off", "err", err)
			p.evalLater(1 * time.Second)
		}
	}()

	if p.Info().State.Freeze != nil {
		return ErrClusterFrozen
	}

	// In order to declare a new generation, we'll need to fetch our current
	// transaction log position, which requires that database be online. In most
	// cases, it will be, since we only declare a new generation as a primary or
	// a caught-up sync. During initial startup, however, we may find out
	// simultaneously that we're the primary or sync AND that the other is gone,
	// so we may attempt to declare a new generation before we've started
	// the database. In this case, this step will fail, but we'll just skip the
	// takeover attempt until the database is running.
	if !*p.online {
		return ErrDatabaseOffline
	}
	wal, err := p.db.XLogPosition()
	if err != nil {
		return err
	}
	if x, err := p.db.XLog().Compare(wal, minWAL); err != nil || x < 0 {
		if err == nil {
			log.Warn("would attempt takeover but not caught up with primary yet", "found_wal", wal)
			err = ErrPeerNotCaughtUp
		}
		return err
	}
	p.updatingState.InitWAL = wal
	log.Info("declaring new generation")

	if err := p.putClusterState(); err != nil {
		return err
	}

	p.setState(p.updatingState)
	p.updatingState = nil
	p.generation = p.Info().State.Generation
	log.Info("declared new generation", "generation", p.Info().State.Generation)

	// assumePrimary() calls evalClusterState() to catch any
	// changes we missed while we were updating.
	p.assumePrimary()

	return nil
}

// As the primary, converts the current cluster to normal mode from singleton
// mode.
func (p *Peer) startTransitionToNormalMode() {
	log := p.log.New("fn", "startTransitionToNormalMode")
	if p.Info().State.Primary.Meta[p.idKey] != p.id || p.Info().Role != RolePrimary {
		panic("startTransitionToNormalMode called when not primary")
	}

	// In the normal takeover case, we'd pick an async. In this case, we take
	// any other peer because we know none of them has anything replicated.
	var newSync *discoverd.Instance
	for _, peer := range p.Info().Peers {
		if peer.Meta[p.idKey] != p.id {
			newSync = peer
		}
	}
	if newSync == nil {
		log.Warn("would takeover but no peers present")
		return
	}
	newAsync := make([]*discoverd.Instance, 0, len(p.Info().Peers))
	for _, a := range p.Info().Peers {
		if a.Meta[p.idKey] != p.id && a.Meta[p.idKey] != newSync.Meta[p.idKey] {
			newAsync = append(newAsync, a)
		}
	}

	log.Debug("transitioning to normal mode")
	p.startTakeoverWithPeer("transitioning to normal mode", p.db.XLog().Zero(), &State{
		Sync:  newSync,
		Async: newAsync,
	})
}

func (p *Peer) startUpdateAsyncs(newAsync []*discoverd.Instance) {
	if p.updatingState != nil {
		panic("startUpdateAsyncs with existing update state")
	}
	log := p.log.New("fn", "startUpdateAsyncs")

	state := p.Info().State
	p.updatingState = &State{
		Generation: state.Generation,
		Primary:    state.Primary,
		Sync:       state.Sync,
		Async:      newAsync,
		Deposed:    state.Deposed,
		InitWAL:    state.InitWAL,
	}
	log.Info("updating list of asyncs")
	if err := p.putClusterState(); err != nil {
		log.Error("failed to update cluster state", "err", err)
	} else {
		p.setState(p.updatingState)
	}
	p.updatingState = nil

	p.triggerEval()
}

// Reconfigure database based on the current configuration. During
// reconfiguration, new requests to reconfigure will be ignored, and incoming
// cluster state changes will be recorded but otherwise ignored. When
// reconfiguration completes, if the desired configuration has changed, we'll
// take another lap to apply the updated configuration.
func (p *Peer) applyConfig() (err error) {
	p.moving()
	log := p.log.New("fn", "applyConfig")

	if p.online == nil {
		panic("applyConfig with database in unknown state")
	}

	config := p.Config()
	if p.applied != nil && p.applied.Equal(config) {
		log.Info("skipping config apply, no changes")
		return nil
	}

	defer func() {
		if err == nil {
			return
		}

		// This is a very unexpected error, and it's very unclear how to deal
		// with it. If we're the primary or sync, we might be tempted to
		// abdicate our position. But without understanding the failure mode,
		// there's no reason to believe any other peer is in a better position
		// to deal with this, and we don't want to flap unnecessarily. So just
		// log an error and try again shortly.
		log.Error("error applying database config", "err", err)
		t := TimeNow()
		p.setRetryPending(&t)
		p.applyConfigLater(1 * time.Second)
	}()

	log.Info("reconfiguring database")
	if err := p.db.Reconfigure(config); err != nil {
		return err
	}

	if config.Role != RoleNone {
		if *p.online {
			log.Debug("skipping start, already online")
		} else {
			log.Debug("starting database")
			if err := p.db.Start(); err != nil {
				return err
			}
		}
	} else {
		if *p.online {
			log.Debug("stopping database")
			if err := p.db.Stop(); err != nil {
				return err
			}
		} else {
			log.Debug("skipping stop, already offline")
		}
	}

	log.Info("applied database config")
	p.setRetryPending(nil)
	p.applied = config
	online := config.Role != RoleNone
	p.online = &online

	// Try applying the configuration again in case anything's
	// changed. If not, this will be a no-op.
	p.triggerApplyConfig()
	return nil
}

func (p *Peer) Config() *Config {
	role := p.Info().Role
	switch role {
	case RolePrimary, RoleSync, RoleAsync:
		return &Config{Role: role, Upstream: p.upstream, Downstream: p.downstream}
	case RoleUnassigned, RoleDeposed:
		return &Config{Role: RoleNone}
	default:
		panic(fmt.Sprintf("unexpected role %v", role))
	}
}

// Determine our index in the async peer list. -1 means not present.
func (p *Peer) whichAsync() int {
	for i, a := range p.Info().State.Async {
		if p.id == a.Meta[p.idKey] {
			return i
		}
	}
	return -1
}

// Return the upstream peer for a given one of the async peers
func (p *Peer) lookupUpstream(whichAsync int) *discoverd.Instance {
	if whichAsync == 0 {
		return p.Info().State.Sync
	}
	return p.Info().State.Async[whichAsync-1]
}

// Return the downstream peer for a given one of the async peers
func (p *Peer) lookupDownstream(whichAsync int) *discoverd.Instance {
	async := p.Info().State.Async
	if whichAsync == len(async)-1 {
		return nil
	}
	return async[whichAsync+1]
}

// Returns true if the given other peer appears to be present in the most
// recently received list of present peers.
func (p *Peer) peerIsPresent(other *discoverd.Instance) bool {
	// We should never even be asking whether we're present. If we need to do
	// this at some point in the future, we need to consider we should always
	// consider ourselves present or whether we should check the list.
	if other.Meta[p.idKey] == p.id {
		panic("peerIsPresent with self")
	}
	for _, peer := range p.Info().Peers {
		if peer.Meta[p.idKey] == other.Meta[p.idKey] {
			return true
		}
	}

	return false
}

func (p *Peer) putClusterState() error {
	s := &DiscoverdState{State: p.updatingState, Index: p.stateIndex}
	if err := p.discoverd.SetState(s); err != nil {
		return err
	}
	p.stateIndex = s.Index
	return nil
}
