package main

import (
	"errors"
	"fmt"
	"path"
	"sort"

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
	s.sortInitialRoutes(initialRoutes)

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

// sortInitialRoutes sorts the given initial routes by domain and path so that
// we process root-path routes before sub-path routes (otherwise we'd get a
// consistency violation adding a sub-path when the root-path doesn't yet
// exist)
func (s *Syncer) sortInitialRoutes(routes []*router.Route) {
	sort.Sort(sortInitialRoutes(routes))
}

type sortInitialRoutes []*router.Route

func (r sortInitialRoutes) Len() int { return len(r) }
func (r sortInitialRoutes) Less(i, j int) bool {
	return path.Join(r[i].Domain, r[i].Path) < path.Join(r[j].Domain, r[j].Path)
}
func (r sortInitialRoutes) Swap(i, j int) { r[i], r[j] = r[j], r[i] }
