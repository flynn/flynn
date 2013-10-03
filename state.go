package main

import (
	"sync"
	"time"

	"github.com/flynn/lorne/types"
	"github.com/flynn/sampi/types"
)

type State struct {
	state map[string]lorne.Job
	mtx   sync.RWMutex
}

func NewState() *State {
	return &State{state: make(map[string]lorne.Job)}
}

func (s *State) AddJob(job *sampi.Job) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.state[job.ID] = lorne.Job{Job: job}
	// TODO: fire event
}

func (s *State) GetJob(id string) lorne.Job {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.state[id]
}

func (s *State) Get() map[string]lorne.Job {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	res := make(map[string]lorne.Job, len(s.state))
	for k, v := range s.state {
		res[k] = v
	}
	return res
}

func (s *State) SetJobStatus(id string, status lorne.JobStatus) {
	// TODO: enforce valid transitions?
	s.mtx.Lock()
	defer s.mtx.Unlock()

	job, ok := s.state[id]
	if !ok {
		return
	}

	switch status {
	case lorne.StatusRunning:
		job.StartedAt = time.Now().UTC()
	case lorne.StatusDone, lorne.StatusCrashed:
		job.EndedAt = time.Now().UTC()
	}
	job.Status = status
	s.state[id] = job

	// TODO: fire event
}

// TODO: prune old jobs?
