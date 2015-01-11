package server

import (
	"container/list"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/flynn/flynn/pkg/stream"
)

type EventKind uint

const (
	EventKindUp EventKind = 1 << iota
	EventKindUpdate
	EventKindDown
	EventKindLeader
	EventKindAll     = ^EventKind(0)
	EventKindUnknown = EventKind(0)
)

var eventKindStrings = map[EventKind]string{
	EventKindUp:      "up",
	EventKindUpdate:  "update",
	EventKindDown:    "down",
	EventKindLeader:  "leader",
	EventKindUnknown: "unknown",
}

func (k EventKind) String() string {
	if s, ok := eventKindStrings[k]; ok {
		return s
	}
	return eventKindStrings[EventKindUnknown]
}

var eventKindMarshalJSON = make(map[EventKind][]byte, len(eventKindStrings))
var eventKindUnmarshalJSON = make(map[string]EventKind, len(eventKindStrings))

func init() {
	for k, s := range eventKindStrings {
		json := `"` + s + `"`
		eventKindMarshalJSON[k] = []byte(json)
		eventKindUnmarshalJSON[json] = k
	}
}

func (k EventKind) MarshalJSON() ([]byte, error) {
	data, ok := eventKindMarshalJSON[k]
	if ok {
		return data, nil
	}
	return eventKindMarshalJSON[EventKindUnknown], nil
}

func (k *EventKind) UnmarshalJSON(data []byte) error {
	if kind, ok := eventKindUnmarshalJSON[string(data)]; ok {
		*k = kind
	}
	return nil
}

type Event struct {
	Service   string    `json:"service"`
	Kind      EventKind `json:"kind"`
	*Instance `json:"instance"`
}

func (e *Event) String() string {
	return fmt.Sprintf("[%s] %s %#v", e.Service, e.Kind, e.Instance)
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
	// Hex(MD5(Proto + "-" + Addr)) but this may change in the future.
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

func (inst *Instance) Valid() error {
	if err := inst.validProto(); err != nil {
		return err
	}
	if _, _, err := net.SplitHostPort(inst.Addr); err != nil {
		return err
	}
	if expected := inst.id(); inst.ID != expected {
		return fmt.Errorf("discoverd: instance id is incorrect, expected %s", expected)
	}
	return nil
}

var ErrUnsetProto = errors.New("discoverd: proto must be set")
var ErrInvalidProto = errors.New("discoverd: proto must be lowercase alphanumeric")

func (inst *Instance) validProto() error {
	if inst.Proto == "" {
		return ErrUnsetProto
	}
	for _, r := range inst.Proto {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ErrInvalidProto
		}
	}
	return nil
}

func (inst *Instance) id() string {
	return md5sum(inst.Proto + "-" + inst.Addr)
}

func md5sum(data string) string {
	digest := md5.Sum([]byte(data))
	return hex.EncodeToString(digest[:])
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

var ErrUnsetService = errors.New("discoverd: service name must not be empty")
var ErrInvalidService = errors.New("discoverd: service must be lowercase alphanumeric plus dash")

func ValidServiceName(service string) error {
	if service == "" {
		return ErrUnsetService
	}
	for _, r := range service {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return ErrInvalidService
		}
	}
	return nil
}

func NewState() *State {
	return &State{
		services:    make(map[string]*service),
		subscribers: make(map[string]*list.List),
	}
}

type State struct {
	// service name -> service
	services map[string]*service
	// TODO: change to atomic.Value and CoW for the services map, and a RWMutex
	// for each service map
	mtx sync.RWMutex

	// service name -> list of *subscriber
	subscribers    map[string]*list.List
	subscribersMtx sync.Mutex
}

type service struct {
	// instance ID -> instance
	instances map[string]*Instance
}

func (s *State) AddService(name string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if _, ok := s.services[name]; !ok {
		s.services[name] = &service{instances: make(map[string]*Instance)}
	}
}

func (s *State) RemoveService(name string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for _, inst := range s.services[name].instances {
		s.broadcast(&Event{
			Service:  name,
			Kind:     EventKindDown,
			Instance: inst,
		})
	}
	delete(s.services, name)
}

func (s *State) AddInstance(serviceName string, inst *Instance) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	data, ok := s.services[serviceName]
	if !ok {
		data = &service{instances: make(map[string]*Instance)}
		s.services[serviceName] = data
	}
	old, existing := data.instances[inst.ID]
	data.instances[inst.ID] = inst

	if !existing || !inst.Equal(old) {
		s.broadcast(&Event{
			Service:  serviceName,
			Kind:     eventKindUpdate(existing),
			Instance: inst,
		})
	}
}

func (s *State) RemoveInstance(serviceName, id string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	data, ok := s.services[serviceName]
	if !ok {
		return
	}
	inst, exists := data.instances[id]
	if !exists {
		return
	}
	delete(data.instances, id)

	s.broadcast(&Event{
		Service:  serviceName,
		Kind:     EventKindDown,
		Instance: inst,
	})
}

func (s *State) SetService(serviceName string, data []*Instance) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	var newData map[string]*Instance
	oldService, ok := s.services[serviceName]
	if data == nil {
		delete(s.services, serviceName)
	} else {
		newData = make(map[string]*Instance, len(data))
		for _, inst := range data {
			newData[inst.ID] = inst
		}
		s.services[serviceName] = &service{instances: newData}
	}
	if !ok {
		// Service doesn't currently exist, send updates for each instance
		for _, inst := range data {
			s.broadcast(&Event{
				Service:  serviceName,
				Kind:     EventKindUp,
				Instance: inst,
			})
		}
		return
	}

	// diff existing
	for _, inst := range data {
		if old, existing := oldService.instances[inst.ID]; !existing || !inst.Equal(old) {
			s.broadcast(&Event{
				Service:  serviceName,
				Kind:     eventKindUpdate(existing),
				Instance: inst,
			})
		}
	}

	// find deleted
	for k, v := range oldService.instances {
		if _, ok := newData[k]; !ok {
			s.broadcast(&Event{
				Service:  serviceName,
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

func (s *State) ListServices() []string {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	res := make([]string, 0, len(s.services))
	for name := range s.services {
		res = append(res, name)
	}
	return res
}

func (s *State) getLocked(name string) []*Instance {
	data, ok := s.services[name]
	if !ok {
		return nil
	}

	res := make([]*Instance, 0, len(data.instances))
	for _, inst := range data.instances {
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
