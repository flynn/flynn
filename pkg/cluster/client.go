// Package cluster implements a client for the Flynn host service.
package cluster

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/stream"
)

// NewClient uses the default discoverd configuration to create a cluster
// client.
func NewClient() *Client {
	return NewClientWithServices(nil)
}

// A ServiceFunc is a function that takes a service name and returns
// a discoverd.Service.
type ServiceFunc func(name string) discoverd.Service

// NewClientWithServices uses the provided services to find cluster members. If
// services is nil, the default discoverd client is used.
func NewClientWithServices(services ServiceFunc) *Client {
	return NewClientWithHTTP(services, httphelper.RetryClient)
}

func NewClientWithHTTP(services ServiceFunc, hc *http.Client) *Client {
	return newClient(services, hc)
}

// ErrNotFound is returned when a resource is not found (HTTP status 404).
var ErrNotFound = errors.New("cluster: resource not found")

func newClient(services ServiceFunc, hc *http.Client) *Client {
	if services == nil {
		services = discoverd.NewService
	}
	s := services("flynn-host")
	return &Client{s: s, h: hc}
}

// A Client is used to discover members of the flynn-host cluster.
type Client struct {
	s discoverd.Service
	h *http.Client
}

// Host returns the host identified by id.
func (c *Client) Host(id string) (*Host, error) {
	hosts, err := c.Hosts()
	if err != nil {
		return nil, err
	}
	for _, h := range hosts {
		if h.ID() == id {
			return h, nil
		}
	}
	return nil, fmt.Errorf("cluster: unknown host %q", id)
}

// Hosts returns a list of hosts in the cluster.
func (c *Client) Hosts() ([]*Host, error) {
	insts, err := c.s.Instances()
	if err != nil {
		return nil, err
	}
	hosts := make([]*Host, len(insts))
	for i, inst := range insts {
		hosts[i] = NewHost(
			inst.Meta["id"],
			inst.Addr,
			c.h,
			HostTagsFromMeta(inst.Meta),
		)
	}
	return hosts, nil
}

func HostTagsFromMeta(meta map[string]string) map[string]string {
	tags := make(map[string]string, len(meta))
	for k, v := range meta {
		if strings.HasPrefix(k, host.TagPrefix) {
			tags[strings.TrimPrefix(k, host.TagPrefix)] = v
		}
	}
	return tags
}

func (c *Client) StreamHostEvents(ch chan *discoverd.Event) (stream.Stream, error) {
	return c.s.Watch(ch)
}
