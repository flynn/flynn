// Package client provides a client for the router API.
package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/client/dialer"
	"github.com/flynn/flynn/router/types"
)

// New uses the default discoverd client and returns a client.
func New() (Client, error) {
	if err := discoverd.Connect(""); err != nil {
		return nil, err
	}
	return NewWithDiscoverd("", discoverd.DefaultClient), nil
}

// NewWithAddr uses addr as the specified API url and returns a client.
func NewWithAddr(addr string) Client {
	return &client{
		url:  fmt.Sprintf("http://%s", addr),
		http: http.DefaultClient,
	}
}

// NewWithDiscoverd uses the provided discoverd client and returns a client.
func NewWithDiscoverd(name string, dc dialer.DiscoverdClient) Client {
	if name == "" {
		name = "router"
	}
	c := &client{
		dialer: dialer.New(dc, nil),
		url:    fmt.Sprintf("http://%s-api", name),
	}
	c.http = &http.Client{Transport: &http.Transport{Dial: c.dialer.Dial}}
	return c
}

// Client is a client for the router API.
type Client interface {
	// CreateRoute creates a new route.
	CreateRoute(*router.Route) error
	// SetRoute updates an existing route. If the route does not exist, it
	// creates a new one.
	SetRoute(*router.Route) error
	// DeleteRoute deletes the route with the specified id.
	DeleteRoute(id string) error
	// GetRoute returns a route with the specified id.
	GetRoute(id string) (*router.Route, error)
	// ListRoutes returns a list of routes. If parentRef is not empty, routes
	// are filtered by the reference (ex: "controller/apps/myapp").
	ListRoutes(parentRef string) ([]*router.Route, error)
	// Closer allows closing the underlying transport connection.
	io.Closer
}

// ErrNotFound is returned when no route was found.
var ErrNotFound = errors.New("router: route not found")

// HTTPError is returned when the server returns a status code that is different
// from 200, which is normally caused by an error.
type HTTPError struct {
	Response *http.Response
}

func (e HTTPError) Error() string {
	return fmt.Sprintf("router: expected http status 200, got %d", e.Response.StatusCode)
}

type client struct {
	url    string
	dialer dialer.Dialer
	http   *http.Client
}

func (c *client) get(path string, v interface{}) error {
	res, err := c.http.Get(c.url + path)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return ErrNotFound
	}
	if res.StatusCode != 200 {
		return HTTPError{res}
	}
	return json.NewDecoder(res.Body).Decode(v)
}

func (c *client) post(path string, v interface{}) error {
	return c.postJSON("POST", path, v)
}

func (c *client) put(path string, v interface{}) error {
	return c.postJSON("PUT", path, v)
}

func (c *client) postJSON(method string, path string, v interface{}) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, c.url+path, bytes.NewBuffer(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return HTTPError{res}
	}
	return json.NewDecoder(res.Body).Decode(v)
}

func (c *client) delete(path string) error {
	req, err := http.NewRequest("DELETE", c.url+path, nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode == 404 {
		return ErrNotFound
	}
	if res.StatusCode != 200 {
		return HTTPError{res}
	}
	return nil
}

func (c *client) CreateRoute(r *router.Route) error {
	return c.post("/routes", r)
}

func (c *client) SetRoute(r *router.Route) error {
	return c.put("/routes", r)
}

func (c *client) DeleteRoute(id string) error {
	return c.delete("/routes/" + id)
}

func (c *client) GetRoute(id string) (*router.Route, error) {
	res := &router.Route{}
	err := c.get("/routes/"+id, res)
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
	err := c.get(path, &res)
	return res, err
}

func (c *client) Close() error {
	if c.dialer != nil {
		return c.dialer.Close()
	}
	return nil
}
