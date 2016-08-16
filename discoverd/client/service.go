package discoverd

import (
	"encoding/json"
	"fmt"
	"time"

	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/stream"
)

type Service interface {
	Leader() (*Instance, error)
	Instances() ([]*Instance, error)
	Addrs() ([]string, error)
	Leaders(chan *Instance) (stream.Stream, error)
	Watch(events chan *Event) (stream.Stream, error)
	GetMeta() (*ServiceMeta, error)
	SetMeta(*ServiceMeta) error
	SetLeader(string) error
}

type LeaderType string

const (
	LeaderTypeManual LeaderType = "manual"
	LeaderTypeOldest LeaderType = "oldest"
)

type ServiceConfig struct {
	LeaderType LeaderType `json:"leader_type"`
}

func (c *Client) AddService(name string, conf *ServiceConfig) error {
	if conf == nil {
		conf = &ServiceConfig{}
	}
	if conf.LeaderType == "" {
		conf.LeaderType = LeaderTypeOldest
	}
	return runAttempts.RunWithValidator(func() error {
		return c.Put("/services/"+name, conf, nil)
	}, func(err error) bool {
		// TODO(titanous): fix Retry error field to be correct for discoverd
		return !hh.IsObjectExistsError(err)
	})
}

func (c *Client) RemoveService(name string) error {
	return c.Delete("/services/" + name)
}

func (c *Client) Service(name string) Service {
	return newService(c, name)
}

func IsNotFound(err error) bool {
	return hh.IsObjectNotFoundError(err)
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
	return res, s.client.Get(fmt.Sprintf("/services/%s/leader", s.name), res)
}

func (s *service) Instances() ([]*Instance, error) {
	var res []*Instance
	return res, s.client.Get(fmt.Sprintf("/services/%s/instances", s.name), &res)
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

// Leaders sends leader events to the given channel (sending nil when there is
// no leader, for example if there are no instances currently registered).
func (s *service) Leaders(leaders chan *Instance) (stream.Stream, error) {
	events := make(chan *Event)
	eventStream, err := s.client.Stream("GET", fmt.Sprintf("/services/%s/leader", s.name), nil, events)
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

type ServiceMeta struct {
	Data json.RawMessage `json:"data"`

	// LeaderID is the optional ID of an instance to set as a leader when
	// applying the new service metadata. It is only used for writes and is not
	// returned during reads.
	LeaderID string `json:"leader_id,omitempty"`

	// When calling SetMeta, Index is checked against the current index and the
	// set only succeeds if the index is the same. A zero index means the meta
	// does not currently exist.
	Index uint64 `json:"index"`
}

func (s *service) GetMeta() (*ServiceMeta, error) {
	meta := &ServiceMeta{}
	return meta, s.client.Get(fmt.Sprintf("/services/%s/meta", s.name), meta)
}

func (s *service) SetMeta(m *ServiceMeta) error {
	return s.client.Put(fmt.Sprintf("/services/%s/meta", s.name), m, m)
}

func (s *service) SetLeader(id string) error {
	return s.client.Put(fmt.Sprintf("/services/%s/leader", s.name), &Instance{ID: id}, nil)
}
