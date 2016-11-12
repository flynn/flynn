package main

import (
	"sync"
	"time"

	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/stream"
	"gopkg.in/inconshreveable/log15.v2"
)

type Service struct {
	Formations map[utils.FormationKey]struct{}

	name     string
	events   chan *discoverd.Event
	logger   log15.Logger
	stop     chan struct{}
	stopOnce sync.Once
}

func NewService(name string, events chan *discoverd.Event, logger log15.Logger) *Service {
	s := &Service{
		Formations: make(map[utils.FormationKey]struct{}),
		name:       name,
		events:     events,
		logger:     logger,
		stop:       make(chan struct{}),
	}
	go s.watchEvents()
	return s
}

func (s *Service) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *Service) watchEvents() {
	log := s.logger.New("fn", "service.watchEvents", "service", s.name)
	var events chan *discoverd.Event
	var stream stream.Stream
	connect := func() (err error) {
		log.Info("connecting service event stream")
		events = make(chan *discoverd.Event)
		stream, err = discoverd.NewService(s.name).Watch(events)
		if err != nil {
			log.Error("error connecting service event stream", "err", err)
		}
		return
	}

	// make initial connection
	for {
		if err := connect(); err == nil {
			defer stream.Close()
			break
		}
		select {
		case <-s.stop:
			return
		case <-time.After(100 * time.Millisecond):
		}
	}

	for {
	eventLoop:
		for {
			select {
			case event, ok := <-events:
				if !ok {
					break eventLoop
				}
				if event.Kind.Any(discoverd.EventKindUp, discoverd.EventKindUpdate) {
					select {
					case s.events <- event:
					case <-s.stop:
						return
					}
				}
			case <-s.stop:
				return
			}
		}
		log.Warn("service event stream disconnected", "err", stream.Err())
		// keep trying to reconnect, unless we are told to stop
	retryLoop:
		for {
			select {
			case <-s.stop:
				return
			default:
			}

			if err := connect(); err == nil {
				break retryLoop
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}
