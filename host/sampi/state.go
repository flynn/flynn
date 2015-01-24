package sampi

import (
	"fmt"
	"sync"
	"sync/atomic"

	log "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/host/types"
)

type State struct {
	curr atomic.Value

	// Mutex locks next, streams, deleted and nextModified.
	// It is locked when a transaction begins, and unlocked when the transaction
	// is committed or rolled back.
	sync.Mutex
	next         map[string]host.Host
	streams      map[string]chan<- *host.Job
	deleted      map[string]struct{}
	nextModified bool

	listenMtx sync.RWMutex
	listeners map[chan host.HostEvent]struct{}

	logger log.Logger
}

func NewState() *State {
	s := &State{
		listeners: make(map[chan host.HostEvent]struct{}),
		streams:   make(map[string]chan<- *host.Job),
		logger:    log.New("app", "sampi.state"),
	}
	s.curr.Store(make(map[string]host.Host))
	return s
}

func (s *State) Begin() {
	l := s.logger.New("fn", "Begin")
	l.Debug("locking state")
	s.Lock()
	s.next = make(map[string]host.Host, len(s.Get()))
	s.nextModified = false
	s.deleted = make(map[string]struct{})
}

func (s *State) Commit() map[string]host.Host {
	l := s.logger.New("fn", "Commit")
	defer func() {
		l.Debug("unlocking state")
		s.Unlock()
	}()
	l.Debug("checking if hosts have been modified")
	if !s.nextModified {
		l.Debug("dropping since hosts where not modified", "at", "nextModified")
		s.next = nil
		return s.Get()
	}
	// copy hosts that were not modified to next
	l.Debug("hosts where modified", "at", "next")
	next := s.next
	for k, v := range s.Get() {
		if _, deleted := s.deleted[k]; !deleted {
			if _, ok := next[k]; !ok {
				next[k] = v
			}
		}
	}
	l.Debug("storing state", "at", "next")
	s.curr.Store(next)
	s.next = nil
	return next
}

func (s *State) Rollback() map[string]host.Host {
	l := s.logger.New("fn", "Rollback")
	defer func() {
		l.Debug("unlocking state")
		s.Unlock()
	}()
	l.Debug("rolling back")
	s.next = nil
	return s.Get()
}

func (s *State) Get() map[string]host.Host {
	s.logger.Debug("getting hosts", "fn", "Get")
	return s.curr.Load().(map[string]host.Host)
}

func (s *State) AddJobs(hostID string, jobs []*host.Job) error {
	l := s.logger.New("fn", "AddJobs", "host.id", hostID)
	h, ok := s.host(hostID)
	if !ok {
		l.Error("host not found")
		return fmt.Errorf("sampi: Unknown host %s", hostID)
	}

	l.Debug("adding new jobs", "at", "ok")
	newJobs := make([]*host.Job, len(h.Jobs), len(h.Jobs)+len(jobs))
	copy(newJobs, h.Jobs)
	newJobs = append(newJobs, jobs...)
	h.Jobs = newJobs

	s.next[hostID] = h
	l.Debug("marking state as modified")
	s.nextModified = true
	return nil
}

func (s *State) SendJob(hostID string, job *host.Job) {
	l := s.logger.New("fn", "SendJob", "host.id", hostID, "job.id", job.ID)
	if ch, ok := s.streams[hostID]; ok {
		l.Debug("stream found; sending job", "at", "sending_job")
		ch <- job
	} else {
		l.Debug("stream for host not found", "at", "invalid_stream")
	}
}

func (s *State) host(id string) (h host.Host, ok bool) {
	h, ok = s.next[id]
	if !ok {
		h, ok = s.Get()[id]
	}
	return
}

func (s *State) RemoveJobs(hostID string, jobIDs ...string) {
	l := s.logger.New("fn", "RemoveJobs", "host.id", hostID)
	h, ok := s.host(hostID)
	if !ok {
		l.Debug("host not found; aborting")
		return
	}
	l.Debug("removing jobs")
	jobs := make([]*host.Job, 0, len(h.Jobs))
outer:
	for _, job := range h.Jobs {
		for _, id := range jobIDs {
			if job.ID == id {
				l.Debug("removing job", "at", "remove", "job.id", id)
				continue outer
			}
		}
		jobs = append(jobs, job)
	}
	h.Jobs = jobs
	s.next[hostID] = h
	l.Debug("marking state as modified")
	s.nextModified = true
}

func (s *State) HostExists(id string) bool {
	_, exists := s.next[id]
	s.logger.Debug("checking if host exists", "fn", "HostExists", "host.id", id, "exists", exists)
	return exists
}

func (s *State) AddHost(host *host.Host, ch chan<- *host.Job) {
	l := s.logger.New("fn", "AddHost", "host.id", host.ID)
	l.Debug("adding host")
	s.next[host.ID] = *host
	s.streams[host.ID] = ch
	l.Debug("marking state as modified")
	s.nextModified = true
}

func (s *State) RemoveHost(id string) {
	l := s.logger.New("fn", "RemoveHost", "host.id", id)
	l.Debug("removing host", "at", "delete.next")
	delete(s.next, id)
	s.deleted[id] = struct{}{}
	if stream := s.streams[id]; stream != nil {
		l.Debug("closing stream", "at", "close_stream")
		close(stream)
	}
	l.Debug("removing streams", "at", "delete.streams")
	delete(s.streams, id)
	l.Debug("marking state as modified")
	s.nextModified = true
}

func (s *State) AddListener(ch chan host.HostEvent) {
	l := s.logger.New("fn", "AddListener")
	l.Debug("locking listeners")
	s.listenMtx.Lock()
	l.Debug("adding listener")
	s.listeners[ch] = struct{}{}
	l.Debug("unlocking listeners")
	s.listenMtx.Unlock()
}

func (s *State) RemoveListener(ch chan host.HostEvent) {
	l := s.logger.New("fn", "RemoveListener")
	l.Debug("locking listeners")
	s.listenMtx.Lock()
	l.Debug("removing listener")
	delete(s.listeners, ch)
	l.Debug("unlocking listeners")
	s.listenMtx.Unlock()
}

func (s *State) sendEvent(hostID, event string) {
	l := s.logger.New("fn", "sendEvent", "host.id", hostID, "event", event)
	l.Debug("locking listeners")
	s.listenMtx.RLock()
	defer func() {
		l.Debug("unlocking listeners")
		s.listenMtx.RUnlock()
	}()
	l.Debug("sending events to listeners")
	e := host.HostEvent{HostID: hostID, Event: event}
	for ch := range s.listeners {
		ch <- e
	}
}
