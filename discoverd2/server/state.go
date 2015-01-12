package server

import (
	"container/list"
	"errors"
	"sync"

	"github.com/flynn/flynn/discoverd2/client"
	"github.com/flynn/flynn/pkg/stream"
)

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

func newService() *service {
	return &service{
		instances: make(map[string]*discoverd.Instance),
	}
}

type service struct {
	// instance ID -> instance
	instances map[string]*discoverd.Instance

	leaderID string
	// leaderIndex is >0 when set, zero is unset
	leaderIndex uint64
	// notifyLeader is true if there is a new leader and the event has not been
	// broadcasted to subscribers
	notifyLeader bool
}

func (s *service) maybeSetLeader(inst *discoverd.Instance) {
	if s.leaderIndex == 0 || s.leaderIndex > inst.Index {
		s.notifyLeader = s.notifyLeader || inst.ID != s.leaderID
		s.leaderID = inst.ID
		s.leaderIndex = inst.Index
	}
}

func (s *service) maybePickLeader() {
	for _, inst := range s.instances {
		s.maybeSetLeader(inst)
	}
}

func (s *service) AddInstance(inst *discoverd.Instance) *discoverd.Instance {
	old := s.instances[inst.ID]
	s.instances[inst.ID] = inst
	s.maybeSetLeader(inst)
	return old
}

func (s *service) RemoveInstance(id string) *discoverd.Instance {
	inst, ok := s.instances[id]
	if !ok {
		return nil
	}
	delete(s.instances, id)
	if inst.ID == s.leaderID {
		s.leaderID = ""
		s.leaderIndex = 0
		s.maybePickLeader()
	}
	return inst
}

func (s *service) SetInstances(data map[string]*discoverd.Instance) {
	if _, ok := data[s.leaderID]; !ok {
		// the current leader is not in the new set
		s.leaderID = ""
		s.leaderIndex = 0
	}
	s.instances = data
	s.maybePickLeader()
}

func (s *service) BroadcastLeader() *discoverd.Instance {
	if s.notifyLeader {
		s.notifyLeader = false
		return s.instances[s.leaderID]
	}
	return nil
}

func (s *service) Leader() *discoverd.Instance {
	if s == nil {
		return nil
	}
	return s.instances[s.leaderID]
}

func (s *State) AddService(name string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if _, ok := s.services[name]; !ok {
		s.services[name] = newService()
	}
}

func (s *State) RemoveService(name string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for _, inst := range s.services[name].instances {
		s.broadcast(&discoverd.Event{
			Service:  name,
			Kind:     discoverd.EventKindDown,
			Instance: inst,
		})
	}
	delete(s.services, name)
}

func eventKindUpdate(existing bool) discoverd.EventKind {
	if existing {
		return discoverd.EventKindUpdate
	}
	return discoverd.EventKindUp
}

func (s *State) AddInstance(serviceName string, inst *discoverd.Instance) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	data, ok := s.services[serviceName]
	if !ok {
		data = newService()
		s.services[serviceName] = data
	}

	if old := data.AddInstance(inst); old == nil || !inst.Equal(old) {
		s.broadcast(&discoverd.Event{
			Service:  serviceName,
			Kind:     eventKindUpdate(old != nil),
			Instance: inst,
		})
	}
	s.broadcastLeader(serviceName)
}

func (s *State) RemoveInstance(serviceName, id string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	data, ok := s.services[serviceName]
	if !ok {
		return
	}
	inst := data.RemoveInstance(id)
	if inst == nil {
		return
	}

	s.broadcast(&discoverd.Event{
		Service:  serviceName,
		Kind:     discoverd.EventKindDown,
		Instance: inst,
	})
	s.broadcastLeader(serviceName)
}

func (s *State) broadcastLeader(serviceName string) {
	if leader := s.services[serviceName].BroadcastLeader(); leader != nil {
		s.broadcast(&discoverd.Event{
			Service:  serviceName,
			Kind:     discoverd.EventKindLeader,
			Instance: leader,
		})
	}
}

func (s *State) SetService(serviceName string, data []*discoverd.Instance) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	var newData, oldData map[string]*discoverd.Instance
	oldService, ok := s.services[serviceName]
	if ok {
		oldData = oldService.instances
	}
	if data == nil {
		delete(s.services, serviceName)
	} else {
		newData = make(map[string]*discoverd.Instance, len(data))
		for _, inst := range data {
			newData[inst.ID] = inst
		}
		if !ok {
			s.services[serviceName] = &service{}
		}
		s.services[serviceName].SetInstances(newData)
	}
	if !ok {
		// Service doesn't currently exist, send updates for each instance
		for _, inst := range data {
			s.broadcast(&discoverd.Event{
				Service:  serviceName,
				Kind:     discoverd.EventKindUp,
				Instance: inst,
			})
		}
		s.broadcastLeader(serviceName)
		return
	}

	// diff existing
	for _, inst := range data {
		if old, existing := oldData[inst.ID]; !existing || !inst.Equal(old) {
			s.broadcast(&discoverd.Event{
				Service:  serviceName,
				Kind:     eventKindUpdate(existing),
				Instance: inst,
			})
		}
	}

	// find deleted
	for k, v := range oldData {
		if _, ok := newData[k]; !ok {
			s.broadcast(&discoverd.Event{
				Service:  serviceName,
				Kind:     discoverd.EventKindDown,
				Instance: v,
			})
		}
	}

	if len(data) > 0 {
		s.broadcastLeader(serviceName)
	}
}

func (s *State) GetLeader(service string) *discoverd.Instance {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.services[service].Leader()
}

func (s *State) Get(service string) []*discoverd.Instance {
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

func (s *State) getLocked(name string) []*discoverd.Instance {
	data, ok := s.services[name]
	if !ok {
		return nil
	}

	res := make([]*discoverd.Instance, 0, len(data.instances))
	for _, inst := range data.instances {
		res = append(res, inst)
	}
	return res
}

type subscription struct {
	kinds discoverd.EventKind
	ch    chan *discoverd.Event
	err   error

	// the following fields are used by Close to clean up
	el      *list.Element
	service string
	state   *State
	closed  bool
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

	if s.closed {
		return
	}

	l := s.state.subscribers[s.service]
	l.Remove(s.el)
	if l.Len() == 0 {
		delete(s.state.subscribers, s.service)
	}
	close(s.ch)

	s.closed = true
}

func (s *State) Subscribe(service string, sendCurrent bool, kinds discoverd.EventKind, ch chan *discoverd.Event) stream.Stream {
	// Grab a copy of the state if we need it. If we do this later we risk
	// a deadlock as updates are broadcast with mtx and subscribersMtx both
	// locked.
	var current []*discoverd.Instance
	var currentLeader *discoverd.Instance
	getCurrent := sendCurrent && kinds&(discoverd.EventKindUp|discoverd.EventKindLeader) != 0
	if getCurrent {
		s.mtx.RLock()
		current = s.getLocked(service)
		currentLeader = s.services[service].Leader()
	}

	s.subscribersMtx.Lock()
	defer s.subscribersMtx.Unlock()

	if getCurrent {
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

	if kinds&discoverd.EventKindUp != 0 {
		for _, inst := range current {
			ch <- &discoverd.Event{
				Service:  service,
				Kind:     discoverd.EventKindUp,
				Instance: inst,
			}
			// TODO: add a timeout to sends so that clients can't slow things down too much
		}
	}
	if kinds&discoverd.EventKindLeader != 0 && currentLeader != nil {
		ch <- &discoverd.Event{
			Service:  service,
			Kind:     discoverd.EventKindLeader,
			Instance: currentLeader,
		}
	}
	if sendCurrent && kinds&discoverd.EventKindCurrent != 0 {
		ch <- &discoverd.Event{
			Service: service,
			Kind:    discoverd.EventKindCurrent,
		}
	}

	return sub
}

var ErrSendBlocked = errors.New("discoverd: channel send failed due to blocked receiver")

func (s *State) broadcast(event *discoverd.Event) {
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
