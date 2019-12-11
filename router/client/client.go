// Package client provides a client for the router API.
package client

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/stream"
	router "github.com/flynn/flynn/router/types"
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
		HTTP:        httphelper.RetryClient,
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
	// StreamEvents streams router events with the given options
	StreamEvents(opts *router.StreamEventsOptions, output chan *router.StreamEvent) (stream.Stream, error)
}

func (c *client) StreamEvents(opts *router.StreamEventsOptions, output chan *router.StreamEvent) (stream.Stream, error) {
	if opts == nil {
		opts = &router.StreamEventsOptions{
			EventTypes: []router.EventType{router.EventTypeRouteSet, router.EventTypeRouteRemove},
		}
	}
	types := make([]string, len(opts.EventTypes))
	for i, t := range opts.EventTypes {
		types[i] = string(t)
	}
	return c.ResumingStream("GET", "/events?types="+strings.Join(types, ","), output)
}
