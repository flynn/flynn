package discoverd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/flynn/flynn/pkg/httpclient"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/stream"
)

type Service interface {
	Leader() (*Instance, error)
	Instances() ([]*Instance, error)
	Addrs() ([]string, error)
	Leaders(chan *Instance) (stream.Stream, error)
	Watch(events chan *Event) (stream.Stream, error)
}

var ErrTimedOut = errors.New("discoverd: timed out waiting for instances")

type Client struct {
	c *httpclient.Client
}

func NewClient() *Client {
	url := os.Getenv("DISCOVERD")
	if url == "" {
		url = "http://127.0.0.1:1111"
	}
	return NewClientWithURL(url)
}

func NewClientWithURL(url string) *Client {
	if !strings.HasPrefix(url, "http") {
		url = "http://" + url
	}
	return &Client{
		c: &httpclient.Client{
			URL:  url,
			HTTP: http.DefaultClient,
		},
	}
}

func (c *Client) Ping() error {
	return c.c.Get("/ping", nil)
}

func (c *Client) AddService(name string) error {
	return c.c.Put("/services/"+name, nil, nil)
}

func (c *Client) RemoveService(name string) error {
	return c.c.Delete("/services/" + name)
}

func (c *Client) Service(name string) Service {
	return newService(c, name)
}

func IsNotFound(err error) bool {
	je, ok := err.(hh.JSONError)
	return ok && je.Code == hh.ObjectNotFoundError
}

func (c *Client) Instances(service string, timeout time.Duration) ([]*Instance, error) {
	s := c.Service(service)
	instances, err := s.Instances()
	if len(instances) > 0 || err != nil && !IsNotFound(err) {
		return instances, err
	}

	events := make(chan *Event)
	stream, err := s.Watch(events)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	// get any current instances
outer:
	for event := range events {
		switch event.Kind {
		case EventKindCurrent:
			break outer
		case EventKindUp:
			instances = append(instances, event.Instance)
		}
	}
	if len(instances) > 0 {
		return instances, nil
	}
	// wait for an instance to come up
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return nil, stream.Err()
			}
			if event.Kind != EventKindUp {
				continue
			}
			return []*Instance{event.Instance}, nil
		case <-time.After(timeout):
			return nil, ErrTimedOut
		}
	}
}

type service struct {
	client *Client
	name   string
}

func newService(client *Client, name string) Service {
	return &service{
		client: client,
		name:   name,
	}
}

func (s *service) Leader() (*Instance, error) {
	res := &Instance{}
	return res, s.client.c.Get(fmt.Sprintf("/services/%s/leader", s.name), res)
}

func (s *service) Instances() ([]*Instance, error) {
	var res []*Instance
	return res, s.client.c.Get(fmt.Sprintf("/services/%s/instances", s.name), &res)
}

func (s *service) Addrs() ([]string, error) {
	instances, err := s.Instances()
	if err != nil {
		return nil, err
	}
	addrs := make([]string, len(instances))
	for i, inst := range instances {
		addrs[i] = inst.Addr
	}
	return addrs, nil
}

func (s *service) Leaders(leaders chan *Instance) (stream.Stream, error) {
	events := make(chan *Event)
	eventStream, err := s.client.c.Stream("GET", fmt.Sprintf("/services/%s/leader", s.name), nil, events)
	if err != nil {
		return nil, err
	}
	stream := stream.New()
	go func() {
		defer func() {
			eventStream.Close()
			// wait for stream to close to prevent race with Err read
			for range events {
			}
			if err := eventStream.Err(); err != nil {
				stream.Error = err
			}
			close(leaders)
		}()
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.Kind != EventKindLeader {
					continue
				}
				select {
				case leaders <- event.Instance:
				case <-stream.StopCh:
					return
				}
			case <-stream.StopCh:
				return
			}
		}
	}()
	return stream, nil
}

func (s *service) Watch(events chan *Event) (stream.Stream, error) {
	return s.client.c.Stream("GET", fmt.Sprintf("/services/%s", s.name), nil, events)
}
