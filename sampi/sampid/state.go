package main

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/flynn/sampi/types"
)

type State struct {
	sync.Mutex
	curr *map[string]sampi.Host
	next *map[string]sampi.Host

	streams map[string]chan<- *sampi.Job

	deleted      map[string]struct{}
	nextModified bool
}

func NewState() *State {
	curr := make(map[string]sampi.Host)
	return &State{curr: &curr, streams: make(map[string]chan<- *sampi.Job)}
}

func (s *State) Begin() {
	s.Lock()
	next := make(map[string]sampi.Host, len(*s.curr))
	s.next = &next
	s.nextModified = false
	s.deleted = make(map[string]struct{})
}

func (s *State) Commit() map[string]sampi.Host {
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

func (s *State) Rollback() map[string]sampi.Host {
	defer s.Unlock()
	s.next = nil
	return *s.curr
}

func (s *State) Get() map[string]sampi.Host {
	return *(*map[string]sampi.Host)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&s.curr))))
}

func (s *State) AddJob(hostID string, job *sampi.Job) bool {
	host, ok := s.host(hostID)
	if !ok {
		return false
	}
	if !host.Add(job) {
		return false
	}
	(*s.next)[hostID] = host
	s.nextModified = true
	return true
}

func (s *State) SendJob(host string, job *sampi.Job) {
	if ch, ok := s.streams[host]; ok {
		ch <- job
	}
}

func (s *State) host(id string) (host sampi.Host, ok bool) {
	host, ok = (*s.next)[id]
	if !ok {
		host, ok = (*s.curr)[id]
	}
	return
}

func (s *State) RemoveJobs(hostID string, jobIDs ...string) {
	host, ok := s.host(hostID)
	if !ok {
		return
	}
	jobs := make([]*sampi.Job, 0, len(host.Jobs))
outer:
	for _, job := range host.Jobs {
		for _, id := range jobIDs {
			if job.ID == id {
				for k, v := range job.Resources {
					if r, ok := host.Resources[k]; ok {
						r.Value += v
						host.Resources[k] = r
					}
				}
				continue outer
			}
		}
		jobs = append(jobs, job)
	}
	host.Jobs = jobs
	(*s.next)[hostID] = host
	s.nextModified = true
}

func (s *State) AddHost(host *sampi.Host, ch chan<- *sampi.Job) {
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
