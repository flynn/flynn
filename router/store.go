package main

import (
	"encoding/json"

	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	discoverd "github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/stream"
	router "github.com/flynn/flynn/router/types"
)

type Store interface {
	List() ([]*router.Route, error)
	Watch(ch chan *router.Event) (stream.Stream, error)
}

func NewControllerStore() (*ControllerStore, error) {
	instances, err := discoverd.NewService("controller").Instances()
	if err != nil {
		return nil, err
	}
	inst := instances[0]
	client, err := controller.NewClient("http://"+inst.Addr, inst.Meta["AUTH_KEY"])
	if err != nil {
		return nil, err
	}
	return &ControllerStore{client}, nil
}

type ControllerStore struct {
	client controller.Client
}

func (c *ControllerStore) List() ([]*router.Route, error) {
	return c.client.RouteList()
}

func (c *ControllerStore) Watch(ch chan *router.Event) (stream.Stream, error) {
	events := make(chan *ct.Event)
	eventStream, err := c.client.StreamEvents(ct.StreamEventsOptions{
		ObjectTypes: []ct.EventType{
			ct.EventTypeRoute,
			ct.EventTypeRouteDeletion,
		},
	}, events)
	if err != nil {
		return nil, err
	}
	routeStream := stream.New()
	go func() {
		defer close(ch)
		defer eventStream.Close()
		for {
			select {
			case event, ok := <-events:
				if !ok {
					routeStream.Error = eventStream.Err()
					return
				}
				var route router.Route
				if err := json.Unmarshal(event.Data, &route); err != nil {
					routeStream.Error = err
					return
				}
				routerEvent := &router.Event{
					Event: c.toRouterEventType(event.ObjectType),
					ID:    route.ID,
					Route: &route,
				}
				select {
				case ch <- routerEvent:
				case <-routeStream.StopCh:
					return
				}
			case <-routeStream.StopCh:
				return
			}
		}
	}()
	return routeStream, nil
}

func (c *ControllerStore) toRouterEventType(typ ct.EventType) router.EventType {
	switch typ {
	case ct.EventTypeRoute:
		return router.EventTypeRouteSet
	case ct.EventTypeRouteDeletion:
		return router.EventTypeRouteRemove
	default:
		return router.EventType("")
	}
}
