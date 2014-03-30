package resource

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-discoverd/balancer"
)

func NewServer(uri string) (*Server, error) {
	if err := discoverd.Connect(""); err != nil {
		return nil, err
	}
	return NewServerWithDiscoverd(uri, discoverd.DefaultClient)
}

type DiscoverdClient interface {
	NewServiceSet(name string) (discoverd.ServiceSet, error)
}

func NewServerWithDiscoverd(uri string, d DiscoverdClient) (*Server, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "discoverd+http" {
		return nil, errors.New("resource: uri scheme must be discoverd+http")
	}

	set, err := d.NewServiceSet(u.Host)
	if err != nil {
		return nil, err
	}
	s := &Server{
		path: u.Path,
		set:  set,
		lb:   balancer.Random(set, nil),
	}
	return s, err
}

type Server struct {
	path string
	set  discoverd.ServiceSet
	lb   balancer.LoadBalancer
}

type Resource struct {
	ID  string            `json:"id"`
	Env map[string]string `json:"env"`
}

func (s *Server) Provision(config []byte) (*Resource, error) {
	server, err := s.lb.Next()
	if err != nil {
		return nil, err
	}

	res, err := http.Post(fmt.Sprintf("http://%s%s", server.Addr, s.path), "", bytes.NewBuffer(config))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("resource: unexpected status code %s", res.StatusCode)
	}

	resource := &Resource{}
	if err := json.NewDecoder(res.Body).Decode(resource); err != nil {
		return nil, err
	}
	return resource, nil
}

func (s *Server) Close() error {
	return s.set.Close()
}
