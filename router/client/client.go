package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/client/dialer"
	"github.com/flynn/flynn/router/types"
)

func New() (Client, error) {
	if err := discoverd.Connect(""); err != nil {
		return nil, err
	}
	return NewWithDiscoverd("", discoverd.DefaultClient), nil
}

func NewWithAddr(addr string) Client {
	return &client{
		url:  fmt.Sprintf("http://%s", addr),
		http: http.DefaultClient,
	}
}

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

type Client interface {
	CreateRoute(*router.Route) error
	SetRoute(*router.Route) error
	DeleteRoute(id string) error
	GetRoute(id string) (*router.Route, error)
	ListRoutes(parentRef string) ([]*router.Route, error)
	PauseService(typ, name string, pause bool) error
	StreamServiceDrain(typ, name string) (io.ReadCloser, error)
	Close() error
}

var ErrNotFound = errors.New("router: route not found")

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

func (c *client) PauseService(t, name string, pause bool) error {
	q := &router.PauseReq{Paused: pause}
	return c.put(fmt.Sprintf("/services/%s/%s", t, name), q)
}

func toJSON(v interface{}) (io.Reader, error) {
	data, err := json.Marshal(v)
	return bytes.NewBuffer(data), err
}

func (c *client) rawReq(method, path string, contentType string, in, out interface{}) (*http.Response, error) {
	var payload io.Reader
	switch v := in.(type) {
	case io.Reader:
		payload = v
	case nil:
	default:
		var err error
		payload, err = toJSON(in)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, c.url+path, payload)
	if err != nil {
		return nil, err
	}
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == 404 {
		res.Body.Close()
		return res, ErrNotFound
	}
	if res.StatusCode == 400 {
		var body ct.ValidationError
		defer res.Body.Close()
		if err = json.NewDecoder(res.Body).Decode(&body); err != nil {
			return res, err
		}
		return res, body
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return res, &url.Error{
			Op:  req.Method,
			URL: req.URL.String(),
			Err: fmt.Errorf("router: unexpected status %d", res.StatusCode),
		}
	}
	if out != nil {
		defer res.Body.Close()
		return res, json.NewDecoder(res.Body).Decode(out)
	}
	return res, nil
}

func (c *client) StreamServiceDrain(t, id string) (io.ReadCloser, error) {
	path := fmt.Sprintf("/services/%s/%s/drain", t, id)
	res, err := c.rawReq("GET", path, "", nil, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

func (c *client) Close() error {
	if c.dialer != nil {
		return c.dialer.Close()
	}
	return nil
}
