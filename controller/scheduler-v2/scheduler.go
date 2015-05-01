package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/host/types"
)

type Host interface {
	ID() string
	ListJobs() (map[string]host.ActiveJob, error)
}

type Cluster interface {
	ListHosts() ([]Host, error)
}

type Scheduler struct {
	cluster Cluster
	log     log15.Logger

	jobs    map[string]*host.ActiveJob
	jobsMtx sync.RWMutex

	listeners map[chan *Event]struct{}
	listenMtx sync.RWMutex

	stop     chan struct{}
	stopOnce sync.Once
}

func NewScheduler(cluster Cluster) *Scheduler {
	return &Scheduler{
		cluster:   cluster,
		log:       log15.New("component", "scheduler"),
		jobs:      make(map[string]*host.ActiveJob),
		listeners: make(map[chan *Event]struct{}),
		stop:      make(chan struct{}),
	}
}

func (s *Scheduler) Run() error {
	log := s.log.New("fn", "Run")
	log.Info("starting scheduler loop")
	defer log.Info("exiting scheduler loop")

	for {
		// check if we should stop first
		select {
		case <-s.stop:
			return nil
		default:
		}

		log.Info("starting cluster sync")
		if err := s.Sync(); err != nil {
			log.Error("error performing cluster sync", "err", err)
			continue
		}
		log.Info("finished cluster sync")

		// TODO: watch events
		select {
		case <-s.stop:
			return nil
		case <-time.After(time.Second):
		}
	}
	return nil
}

func (s *Scheduler) Sync() error {
	log := s.log.New("fn", "Sync")

	defer s.sendEvent(&Event{Type: EventTypeClusterSync})

	log.Info("getting host list")
	hosts, err := s.cluster.ListHosts()
	if err != nil {
		log.Error("error getting host list", "err", err)
		return err
	}
	log.Info(fmt.Sprintf("got %d hosts", len(hosts)))

	s.jobsMtx.Lock()
	defer s.jobsMtx.Unlock()
	for _, h := range hosts {
		log = log.New("host_id", h.ID())
		log.Info("getting jobs list")
		var jobs map[string]host.ActiveJob
		jobs, err = h.ListJobs()
		if err != nil {
			log.Error("error getting jobs list", "err", err)
			continue
		}
		log.Info(fmt.Sprintf("got %d jobs", len(jobs)))
		for id, job := range jobs {
			log.Info(fmt.Sprintf("adding job with ID %q", id))
			s.jobs[id] = &job
		}
	}
	return err
}

func (s *Scheduler) Stop() error {
	s.log.Info("stopping scheduler loop", "fn", "Stop")
	s.stopOnce.Do(func() { close(s.stop) })
	return nil
}

func (s *Scheduler) GetJob(id string) *host.ActiveJob {
	s.jobsMtx.RLock()
	defer s.jobsMtx.RUnlock()
	return s.jobs[id]
}

func (s *Scheduler) Subscribe(events chan *Event) *Stream {
	s.log.Info("adding subscriber", "fn", "Subscribe")
	s.listenMtx.Lock()
	defer s.listenMtx.Unlock()
	s.listeners[events] = struct{}{}
	return &Stream{s, events}
}

func (s *Scheduler) Unsubscribe(events chan *Event) {
	s.log.Info("removing subscriber", "fn", "Unsubscribe")
	s.listenMtx.Lock()
	defer s.listenMtx.Unlock()
	delete(s.listeners, events)
}

type Stream struct {
	s      *Scheduler
	events chan *Event
}

func (s *Stream) Close() error {
	s.s.Unsubscribe(s.events)
	return nil
}

func (s *Scheduler) sendEvent(event *Event) {
	s.listenMtx.RLock()
	defer s.listenMtx.RUnlock()
	s.log.Info(fmt.Sprintf("sending %q event to %d listeners", event.Type, len(s.listeners)))
	for ch := range s.listeners {
		// TODO: handle slow listeners
		ch <- event
	}
}

type Event struct {
	Type EventType
}

type EventType string

const (
	EventTypeClusterSync EventType = "cluster-sync"
)
