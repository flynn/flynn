package sampi

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/flynn/flynn-host/types"
)

type State struct {
	sync.Mutex
	curr *map[string]host.Host
	next *map[string]host.Host

	listeners map[chan host.HostEvent]struct{}
	listenMtx sync.RWMutex
	streams   map[string]chan<- *host.Job

	deleted      map[string]struct{}
	nextModified bool
}

func NewState() *State {
	curr := make(map[string]host.Host)
	return &State{
		curr:      &curr,
		listeners: make(map[chan host.HostEvent]struct{}),
		streams:   make(map[string]chan<- *host.Job),
	}
}

func (s *State) Begin() {
	s.Lock()
	next := make(map[string]host.Host, len(*s.curr))
	s.next = &next
	s.nextModified = false
	s.deleted = make(map[string]struct{})
}

func (s *State) Commit() map[string]host.Host {
	defer s.Unlock()
	if !s.nextModified {
		s.next = nil
		return *s.curr
	}
	// copy hosts that were not modified to next
	next := *s.next
	for k, v := range *s.curr {
		if _, deleted := s.deleted[k]; !deleted {
			if _, ok := next[k]; !ok {
				next[k] = v
			}
		}
	}
	// replace curr with next
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&s.curr)), unsafe.Pointer(s.next))
	s.next = nil
	return *s.curr
}

func (s *State) Rollback() map[string]host.Host {
	defer s.Unlock()
	s.next = nil
	return *s.curr
}

func (s *State) Get() map[string]host.Host {
	return *(*map[string]host.Host)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&s.curr))))
}

func (s *State) AddJobs(hostID string, jobs []*host.Job) error {
	h, ok := s.host(hostID)
	if !ok {
		return fmt.Errorf("sampi: Unknown host %s", hostID)
	}

	newJobs := make([]*host.Job, len(h.Jobs), len(h.Jobs)+len(jobs))
	copy(newJobs, h.Jobs)
	newJobs = append(newJobs, jobs...)
	h.Jobs = newJobs

	(*s.next)[hostID] = h
	s.nextModified = true
	return nil
}

func (s *State) SendJob(host string, job *host.Job) {
	if ch, ok := s.streams[host]; ok {
		ch <- job
	}
}

func (s *State) host(id string) (h host.Host, ok bool) {
	h, ok = (*s.next)[id]
	if !ok {
		h, ok = (*s.curr)[id]
	}
	return
}

func (s *State) RemoveJobs(hostID string, jobIDs ...string) {
	h, ok := s.host(hostID)
	if !ok {
		return
	}
	jobs := make([]*host.Job, 0, len(h.Jobs))
outer:
	for _, job := range h.Jobs {
		for _, id := range jobIDs {
			if job.ID == id {
				continue outer
			}
		}
		jobs = append(jobs, job)
	}
	h.Jobs = jobs
	(*s.next)[hostID] = h
	s.nextModified = true
}

func (s *State) HostExists(id string) bool {
	_, exists := (*s.next)[id]
	return exists
}

func (s *State) AddHost(host *host.Host, ch chan<- *host.Job) {
	(*s.next)[host.ID] = *host
	s.streams[host.ID] = ch
	s.nextModified = true
}

func (s *State) RemoveHost(id string) {
	delete(*s.next, id)
	s.deleted[id] = struct{}{}
	delete(s.streams, id)
	s.nextModified = true
}

func (s *State) AddListener(ch chan host.HostEvent) {
	s.listenMtx.Lock()
	s.listeners[ch] = struct{}{}
	s.listenMtx.Unlock()
}

func (s *State) RemoveListener(ch chan host.HostEvent) {
	s.listenMtx.Lock()
	delete(s.listeners, ch)
	s.listenMtx.Unlock()
}

func (s *State) sendEvent(hostID, event string) {
	s.listenMtx.RLock()
	defer s.listenMtx.RUnlock()
	e := host.HostEvent{HostID: hostID, Event: event}
	for ch := range s.listeners {
		ch <- e
	}
}
