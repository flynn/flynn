package server

import (
	"container/list"
	"errors"
	"sync"

	"github.com/flynn/flynn/pkg/stream"
)

type EventKind uint

const (
	EventKindUp EventKind = 1 << iota
	EventKindUpdate
	EventKindDown
	EventKindLeader
)

func (k EventKind) String() string {
	switch k {
	case EventKindUp:
		return "up"
	case EventKindUpdate:
		return "update"
	case EventKindDown:
		return "down"
	case EventKindLeader:
		return "leader"
	default:
		return "unknown"
	}
}

type Event struct {
	Service string
	Kind    EventKind
	*Instance
}

func eventKindUpdate(existing bool) EventKind {
	if existing {
		return EventKindUpdate
	}
	return EventKindUp
}

// Instance is a single running instance of a service.
type Instance struct {
	// ID is unique within the service, and is currently defined as
	// Hex(SHA256(Proto + "-" + Addr)) but this may change in the future.
	ID string `json:"id"`

	// Addr is the IP/port address that can be used to communicate with the
	// service. It must be valid to dial this address.
	Addr string `json:"addr"`

	// Proto is the protocol used to connect to the service, examples include:
	// tcp, udp, http, https. It must be lowercase alphanumeric.
	Proto string `json:"proto"`

	// Meta is arbitrary metadata specified when registering the instance.
	Meta map[string]string `json:"meta,omitempty"`

	// Leader is true if the instance is the leader of the service. Exactly one
	// instance per service has this set to true at any point in time.
	Leader bool `json:"leader,omitempty"`

	// Index is the logical epoch of the initial registration of the instance.
	// It is guaranteed to be unique, not change as long as the instance does
	// not expire, and sort with other indexes in the order of instance
	// creation.
	Index uint `json:"index"`
}

func (inst *Instance) Equal(other *Instance) bool {
	return inst.Addr == other.Addr &&
		inst.Proto == other.Proto &&
		inst.Index == other.Index &&
		mapEqual(inst.Meta, other.Meta)
}

func mapEqual(x, y map[string]string) bool {
	if len(x) != len(y) {
		return false
	}
	for k, v := range x {
		if yv, ok := y[k]; !ok || yv != v {
			return false
		}
	}
	return true
}

func NewState() *State {
	return &State{
		services:    make(map[string]map[string]*Instance),
		subscribers: make(map[string]*list.List),
	}
}

type State struct {
	// service name -> instance ID -> instance
	services map[string]map[string]*Instance
	// TODO: change to atomic.Value and CoW for the services map, and a RWMutex
	// for each service map
	mtx sync.RWMutex

	// service name -> list of *subscriber
	subscribers    map[string]*list.List
	subscribersMtx sync.Mutex
}

func (s *State) AddInstance(service string, inst *Instance) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	data, ok := s.services[service]
	if !ok {
		data = make(map[string]*Instance)
		s.services[service] = data
	}
	old, existing := data[inst.ID]
	data[inst.ID] = inst

	if !existing || !inst.Equal(old) {
		s.broadcast(&Event{
			Service:  service,
			Kind:     eventKindUpdate(existing),
			Instance: inst,
		})
	}
}

func (s *State) RemoveInstance(service, id string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	data, ok := s.services[service]
	if !ok {
		return
	}
	inst, exists := data[id]
	if !exists {
		return
	}
	delete(data, id)

	s.broadcast(&Event{
		Service:  service,
		Kind:     EventKindDown,
		Instance: inst,
	})
}

func (s *State) SetService(service string, data []*Instance) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	oldData, ok := s.services[service]
	if len(data) == 0 {
		delete(s.services, service)
	} else {
		newData := make(map[string]*Instance, len(data))
		for _, inst := range data {
			newData[inst.ID] = inst
		}
		s.services[service] = newData
	}
	if !ok {
		// Service doesn't currently exist, send updates for each instance
		for _, inst := range data {
			s.broadcast(&Event{
				Service:  service,
				Kind:     EventKindUp,
				Instance: inst,
			})
		}
		return
	}

	// diff existing
	for _, inst := range data {
		if old, existing := oldData[inst.ID]; !existing || !inst.Equal(old) {
			s.broadcast(&Event{
				Service:  service,
				Kind:     eventKindUpdate(existing),
				Instance: inst,
			})
		}
	}

	// find deleted
	for k, v := range oldData {
		if _, ok := s.services[service][k]; !ok {
			s.broadcast(&Event{
				Service:  service,
				Kind:     EventKindDown,
				Instance: v,
			})
		}
	}
}

func (s *State) Get(service string) []*Instance {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.getLocked(service)
}

func (s *State) getLocked(service string) []*Instance {
	data, ok := s.services[service]
	if !ok {
		return nil
	}

	res := make([]*Instance, 0, len(data))
	for _, inst := range data {
		res = append(res, inst)
	}
	return res
}

type subscription struct {
	kinds EventKind
	ch    chan *Event
	err   error

	// the following fields are used by Close to clean up
	el      *list.Element
	service string
	state   *State
}

func (s *subscription) Err() error {
	return s.err
}

func (s *subscription) Close() error {
	go func() {
		// drain channel to prevent deadlocks
		for range s.ch {
		}
	}()

	s.close()
	return nil
}

func (s *subscription) close() {
	s.state.subscribersMtx.Lock()
	defer s.state.subscribersMtx.Unlock()
	l := s.state.subscribers[s.service]
	l.Remove(s.el)
	if l.Len() == 0 {
		delete(s.state.subscribers, s.service)
	}
	close(s.ch)
}

func (s *State) Subscribe(service string, sendCurrent bool, kinds EventKind, ch chan *Event) stream.Stream {
	// Grab a copy of the state if we need it. If we do this later we risk
	// a deadlock as updates are broadcast with mtx and subscribersMtx both
	// locked.
	var current []*Instance
	sendCurrent = sendCurrent && kinds&(EventKindUp|EventKindUpdate) != 0
	if sendCurrent {
		s.mtx.RLock()
		current = s.getLocked(service)
	}

	s.subscribersMtx.Lock()
	defer s.subscribersMtx.Unlock()

	if sendCurrent {
		// Make sure we unlock this *after* locking subscribersMtx to prevent any
		// changes from being applied before we send the current state
		s.mtx.RUnlock()
	}

	l, ok := s.subscribers[service]
	if !ok {
		l = list.New()
		s.subscribers[service] = l
	}
	sub := &subscription{
		kinds:   kinds,
		ch:      ch,
		state:   s,
		service: service,
	}
	sub.el = l.PushBack(sub)

	for _, inst := range current {
		ch <- &Event{
			Service:  service,
			Kind:     EventKindUp,
			Instance: inst,
		}
		// TODO: add a timeout here so that clients can't slow things down too much
	}

	return sub
}

var ErrSendBlocked = errors.New("discoverd: channel send failed due to blocked receiver")

func (s *State) broadcast(event *Event) {
	s.subscribersMtx.Lock()
	defer s.subscribersMtx.Unlock()

	l, ok := s.subscribers[event.Service]
	if !ok {
		return
	}

	for e := l.Front(); e != nil; e = e.Next() {
		sub := e.Value.(*subscription)

		// skip if kinds bitmap doesn't include this event type
		if sub.kinds&event.Kind == 0 {
			continue
		}

		select {
		case sub.ch <- event:
		default:
			sub.err = ErrSendBlocked
			// run in a goroutine as it requires a lock on subscribersMtx
			go sub.Close()
		}
	}
}
