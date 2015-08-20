// Package client provides a client for the router API.
package client

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/flynn/flynn/router/types"
)

// ErrNotFound is returned when no route was found.
var ErrNotFound = errors.New("router: route not found")

type client struct {
	*httpclient.Client
}

// New uses the default discoverd client and returns a client.
func New() Client {
	return newRouterClient()
}

// NewWithHTTP does the same thing as New but uses the given *http.Client
func NewWithHTTP(http *http.Client) Client {
	c := newRouterClient()
	http.Transport = c.HTTP.Transport
	c.HTTP = http
	return c
}

func newRouterClient() *client {
	return &client{Client: &httpclient.Client{
		ErrNotFound: ErrNotFound,
		URL:         "http://router-api.discoverd:5000",
		HTTP:        http.DefaultClient,
	}}
}

// NewWithAddr uses addr as the specified API url and returns a client.
func NewWithAddr(addr string) Client {
	c := newRouterClient()
	c.URL = fmt.Sprintf("http://%s", addr)
	return c
}

// Client is a client for the router API.
type Client interface {
	// CreateRoute creates a new route.
	CreateRoute(*router.Route) error
	// UpdateRoute updates an existing route by overwriting all fields on the route
	// except ID and Domain.
	UpdateRoute(*router.Route) error
	// DeleteRoute deletes the route with the specified routeType and id.
	DeleteRoute(routeType, id string) error
	// GetRoute returns a route with the specified routeType and id.
	GetRoute(routeType, id string) (*router.Route, error)
	// ListRoutes returns a list of routes. If parentRef is not empty, routes
	// are filtered by the reference (ex: "controller/apps/myapp").
	ListRoutes(parentRef string) ([]*router.Route, error)
	StreamEvents(output chan *router.StreamEvent) (stream.Stream, error)
}

func (c *client) CreateRoute(r *router.Route) error {
	return c.Post("/routes", r, r)
}

func (c *client) UpdateRoute(r *router.Route) error {
	return c.Put("/routes/"+r.Type+"/"+r.ID, r, r)
}

func (c *client) DeleteRoute(routeType, id string) error {
	return c.Delete("/routes/" + routeType + "/" + id)
}

func (c *client) GetRoute(routeType, id string) (*router.Route, error) {
	res := &router.Route{}
	err := c.Get(fmt.Sprintf("/routes/%s/%s", routeType, id), res)
	return res, err
}

func (c *client) ListRoutes(parentRef string) ([]*router.Route, error) {
	path := "/routes"
	if parentRef != "" {
		q := make(url.Values)
		q.Set("parent_ref", parentRef)
		path += "?" + q.Encode()
	}
	var res []*router.Route
	err := c.Get(path, &res)
	return res, err
}

func (c *client) StreamEvents(output chan *router.StreamEvent) (stream.Stream, error) {
	return c.ResumingStream("GET", "/events", output)
}
