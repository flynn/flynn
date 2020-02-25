package main

import (
	"errors"
	"fmt"

	router "github.com/flynn/flynn/router/types"
	"golang.org/x/net/context"
)

var ErrNotFound = errors.New("router: route not found")

type SyncHandler interface {
	Set(route *router.Route) error
	Remove(id string) error
	Current() map[string]struct{}
}

func NewSyncer(store Store, routeType string) *Syncer {
	return &Syncer{store, routeType}
}

type Syncer struct {
	store     Store
	routeType string
}

func (s *Syncer) Sync(ctx context.Context, h SyncHandler, startc chan<- struct{}) error {
	events := make(chan *router.Event)
	stream, err := s.store.Watch(events)
	if err != nil {
		return err
	}
	defer stream.Close()

	initialRoutes, err := s.store.List()
	if err != nil {
		return err
	}

	toRemove := h.Current()
	for _, route := range initialRoutes {
		if route.Type != s.routeType {
			continue
		}
		if _, ok := toRemove[route.ID]; ok {
			delete(toRemove, route.ID)
		}
		if err := h.Set(route); err != nil {
			return err
		}
	}
	// send remove for any routes that are no longer in the store
	for id := range toRemove {
		if err := h.Remove(id); err != nil {
			return err
		}
	}
	close(startc)

	for {
		select {
		case event, ok := <-events:
			if !ok {
				return stream.Err()
			}
			if event.Route.Type != s.routeType {
				continue
			}
			if err := s.handleUpdate(h, event); err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *Syncer) handleUpdate(h SyncHandler, event *router.Event) error {
	var err error
	switch event.Event {
	case router.EventTypeRouteSet:
		if cert := event.Route.Certificate; cert != nil {
			key, err := s.store.PrivateKey(cert.KeyID())
			if err != nil {
				return err
			}
			cert.Key = key
		}
		err = h.Set(event.Route)
	case router.EventTypeRouteRemove:
		err = h.Remove(event.Route.ID)
		// ignore non-existent routes
		if err == ErrNotFound {
			err = nil
		}
	default:
		err = fmt.Errorf("unknown event type: %v", event.Event)
	}
	return err
}
