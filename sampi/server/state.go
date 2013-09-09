package main

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/flynn/sampi/types"
)

type State struct {
	// Lock to serialize writes
	mtx  sync.Mutex
	curr *map[string]types.Host
	next map[string]types.Host

	nextModified bool
}

func (s *State) Begin() {
	s.mtx.Lock()
	s.nextModified = false
	s.next = make(map[string]types.Host, len(*s.curr))
}

func (s *State) Commit() map[string]types.Host {
	defer s.mtx.Unlock()
	if !s.nextModified {
		return *s.curr
	}
	// copy hosts that were not modified to next
	for k, v := range *s.curr {
		if _, ok := s.next[k]; !ok {
			s.next[k] = v
		}
	}
	// replace curr with next
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&s.curr)), unsafe.Pointer(&s.next))
	return *s.curr
}

func (s *State) Rollback() map[string]types.Host {
	defer s.mtx.Unlock()
	s.next = nil
	return *s.curr
}

func (s *State) Get() map[string]types.Host {
	return *(*map[string]types.Host)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&s.curr))))
}

func (s *State) Add(hostID string, job *types.Job) bool {
	host, ok := s.next[hostID]
	if !ok {
		host, ok = (*s.curr)[hostID]
	}
	if !ok {
		return false
	}
	if !host.Add(job) {
		return false
	}
	s.next[hostID] = host
	s.nextModified = true
	return true
}
