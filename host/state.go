package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/flynn/flynn/host/types"
)

// TODO: prune old jobs?

type State struct {
	id string

	jobs map[string]*host.ActiveJob
	mtx  sync.RWMutex

	containers map[string]*host.ActiveJob              // container ID -> job
	listeners  map[string]map[chan host.Event]struct{} // job id -> listener list (ID "all" gets all events)
	listenMtx  sync.RWMutex
	attachers  map[string]map[chan struct{}]struct{}

	stateFileMtx sync.Mutex
	stateFile    *os.File
	backend      Backend
}

func NewState(id string) *State {
	return &State{
		id:         id,
		jobs:       make(map[string]*host.ActiveJob),
		containers: make(map[string]*host.ActiveJob),
		listeners:  make(map[string]map[chan host.Event]struct{}),
		attachers:  make(map[string]map[chan struct{}]struct{}),
	}
}

func (s *State) Restore(file string, backend Backend) error {
	s.stateFileMtx.Lock()
	defer s.stateFileMtx.Unlock()
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	s.stateFile = f
	s.backend = backend
	d := json.NewDecoder(f)
	if err := d.Decode(&s.jobs); err != nil {
		if err == io.EOF {
			err = nil
		}
		return err
	}
	for _, job := range s.jobs {
		if job.ContainerID != "" {
			s.containers[job.ContainerID] = job
		}
	}
	return backend.RestoreState(s.jobs, d)
}

func (s *State) persist() {
	s.stateFileMtx.Lock()
	defer s.stateFileMtx.Unlock()
	if _, err := s.stateFile.Seek(0, 0); err != nil {
		// log error
		return
	}
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	enc := json.NewEncoder(s.stateFile)
	if err := enc.Encode(s.jobs); err != nil {
		// log error
		return
	}
	if b, ok := s.backend.(StateSaver); ok {
		if err := b.SaveState(enc); err != nil {
			// log error
			return
		}
	}
	if err := s.stateFile.Sync(); err != nil {
		// log error
	}
}

func (s *State) AddJob(j *host.Job, ip string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	job := &host.ActiveJob{Job: j, HostID: s.id, InternalIP: ip}
	s.jobs[j.ID] = job
	s.sendEvent(job, "create")
	go s.persist()
}

func (s *State) GetJob(id string) *host.ActiveJob {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	job := s.jobs[id]
	if job == nil {
		return nil
	}
	jobCopy := *job
	return &jobCopy
}

func (s *State) RemoveJob(id string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	delete(s.jobs, id)
	go s.persist()
}

func (s *State) Get() map[string]host.ActiveJob {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	res := make(map[string]host.ActiveJob, len(s.jobs))
	for k, v := range s.jobs {
		res[k] = *v
	}
	return res
}

func (s *State) ClusterJobs() []*host.Job {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	res := make([]*host.Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		res = append(res, j.Job)
	}
	return res
}

func (s *State) SetContainerID(jobID, containerID string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.jobs[jobID].ContainerID = containerID
	s.containers[containerID] = s.jobs[jobID]
	go s.persist()
}

func (s *State) SetManifestID(jobID, manifestID string) {
	s.mtx.Lock()
	s.jobs[jobID].ManifestID = manifestID
	s.mtx.Unlock()
	go s.persist()
}

func (s *State) SetForceStop(jobID string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return
	}

	job.ForceStop = true
	go s.persist()
}

func (s *State) SetStatusRunning(jobID string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	job, ok := s.jobs[jobID]
	if !ok || job.Status != host.StatusStarting {
		return
	}

	job.StartedAt = time.Now().UTC()
	job.Status = host.StatusRunning
	s.sendEvent(job, "start")
	go s.persist()
}

func (s *State) SetContainerStatusDone(containerID string, exitCode int) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	job, ok := s.containers[containerID]
	if !ok {
		return
	}
	s.setStatusDone(job, exitCode)
}

func (s *State) SetStatusDone(jobID string, exitCode int) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		fmt.Println("SKIP")
		return
	}
	s.setStatusDone(job, exitCode)
}

func (s *State) setStatusDone(job *host.ActiveJob, exitStatus int) {
	if job.Status == host.StatusDone || job.Status == host.StatusCrashed || job.Status == host.StatusFailed {
		return
	}
	job.EndedAt = time.Now().UTC()
	job.ExitStatus = exitStatus
	if exitStatus == 0 {
		job.Status = host.StatusDone
	} else {
		job.Status = host.StatusCrashed
	}
	s.sendEvent(job, "stop")
	go s.persist()
}

func (s *State) SetStatusFailed(jobID string, err error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	job, ok := s.jobs[jobID]
	if !ok || job.Status == host.StatusDone || job.Status == host.StatusCrashed || job.Status == host.StatusFailed {
		return
	}
	job.Status = host.StatusFailed
	job.EndedAt = time.Now().UTC()
	errStr := err.Error()
	job.Error = &errStr
	s.sendEvent(job, "error")
	go s.persist()
	go s.WaitAttach(jobID)
}

func (s *State) AddAttacher(jobID string, ch chan struct{}) *host.ActiveJob {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if job, ok := s.jobs[jobID]; ok {
		jobCopy := *job
		return &jobCopy
	}
	if _, ok := s.attachers[jobID]; !ok {
		s.attachers[jobID] = make(map[chan struct{}]struct{})
	}
	s.attachers[jobID][ch] = struct{}{}
	return nil
}

func (s *State) RemoveAttacher(jobID string, ch chan struct{}) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if a, ok := s.attachers[jobID]; ok {
		delete(a, ch)
		if len(a) == 0 {
			delete(s.attachers, jobID)
		}
	}
}

func (s *State) WaitAttach(jobID string) {
	s.mtx.Lock()
	a := s.attachers[jobID]
	delete(s.attachers, jobID)
	s.mtx.Unlock()
	for ch := range a {
		// signal attach
		ch <- struct{}{}
		// wait for attach
		<-ch
	}
}

func (s *State) AddListener(jobID string) chan host.Event {
	ch := make(chan host.Event)
	s.listenMtx.Lock()
	if _, ok := s.listeners[jobID]; !ok {
		s.listeners[jobID] = make(map[chan host.Event]struct{})
	}
	s.listeners[jobID][ch] = struct{}{}
	s.listenMtx.Unlock()
	return ch
}

func (s *State) RemoveListener(jobID string, ch chan host.Event) {
	go func() {
		// drain to prevent deadlock while removing the listener
		for range ch {
		}
	}()
	s.listenMtx.Lock()
	delete(s.listeners[jobID], ch)
	if len(s.listeners[jobID]) == 0 {
		delete(s.listeners, jobID)
	}
	s.listenMtx.Unlock()
	close(ch)
}

func (s *State) sendEvent(job *host.ActiveJob, event string) {
	j := *job
	go func() {
		s.listenMtx.RLock()
		defer s.listenMtx.RUnlock()
		e := host.Event{JobID: job.Job.ID, Job: &j, Event: event}
		for ch := range s.listeners["all"] {
			ch <- e
		}
		for ch := range s.listeners[job.Job.ID] {
			ch <- e
		}
	}()
}
