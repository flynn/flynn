package discover

import (
	"errors"
	"net"
	"strings"
	"sync"
	"time"
	"os"

	"github.com/flynn/rpcplus"
)

type Service struct {
	Index  int // not used yet
	Name   string
	Host   string
	Port   string
	Addr   string
	Attrs  map[string]string
	Online bool
}

type ServiceSet struct {
	services  map[string]*Service
	filters   map[string]string
	listeners map[chan *ServiceUpdate]struct{}
	serMutex  sync.Mutex
	filMutex  sync.Mutex
	lisMutex  sync.Mutex
}

func (s *ServiceSet) bind(updates chan *ServiceUpdate) chan struct{} {
	// current is an event when enough service updates have been
	// received to bring us to "current" state (when subscribed)
	current := make(chan struct{})
	go func() {
		isCurrent := false
		for update := range updates {
			if update.Addr == "" && update.Name == "" && !isCurrent {
				close(current)
				isCurrent = true
				continue
			}
			if s.filters != nil && !s.matchFilters(update.Attrs) {
				continue
			}
			s.serMutex.Lock()
			if _, exists := s.services[update.Addr]; !exists {
				host, port, _ := net.SplitHostPort(update.Addr)
				s.services[update.Addr] = &Service{
					Name: update.Name,
					Addr: update.Addr,
					Host: host,
					Port: port,
				}
			}
			s.services[update.Addr].Online = update.Online
			s.services[update.Addr].Attrs = update.Attrs
			s.serMutex.Unlock()
			if s.listeners != nil {
				s.lisMutex.Lock()
				for ch := range s.listeners {
					ch <- update
				}
				s.lisMutex.Unlock()
			}
		}
	}()
	return current
}

func (s *ServiceSet) matchFilters(attrs map[string]string) bool {
	s.filMutex.Lock()
	defer s.filMutex.Unlock()
	for key, value := range s.filters {
		if attrs[key] != value {
			return false
		}
	}
	return true
}

func (s *ServiceSet) Online() []*Service {
	s.serMutex.Lock()
	defer s.serMutex.Unlock()
	list := make([]*Service, 0, len(s.services))
	for _, service := range s.services {
		if service.Online {
			list = append(list, service)
		}
	}
	return list
}

func (s *ServiceSet) Offline() []*Service {
	s.serMutex.Lock()
	defer s.serMutex.Unlock()
	list := make([]*Service, 0, len(s.services))
	for _, service := range s.services {
		if !service.Online {
			list = append(list, service)
		}
	}
	return list
}

func (s *ServiceSet) OnlineAddrs() []string {
	list := make([]string, 0, len(s.services))
	for _, service := range s.Online() {
		list = append(list, service.Addr)
	}
	return list
}

func (s *ServiceSet) OfflineAddrs() []string {
	list := make([]string, 0, len(s.services))
	for _, service := range s.Offline() {
		list = append(list, service.Addr)
	}
	return list
}

func (s *ServiceSet) Select(attrs map[string]string) []*Service {
	s.serMutex.Lock()
	defer s.serMutex.Unlock()
	list := make([]*Service, 0, len(s.services))
outer:
	for _, service := range s.services {
		for key, value := range attrs {
			if service.Attrs[key] != value {
				continue outer
			}
		}
		list = append(list, service)
	}
	return list
}

func (s *ServiceSet) Filter(attrs map[string]string) {
	s.filMutex.Lock()
	s.filters = attrs
	s.filMutex.Unlock()
	s.serMutex.Lock()
	defer s.serMutex.Unlock()
	for key, service := range s.services {
		if !s.matchFilters(service.Attrs) {
			delete(s.services, key)
		}
	}
}

func (s *ServiceSet) Subscribe(ch chan *ServiceUpdate) {
	s.lisMutex.Lock()
	defer s.lisMutex.Unlock()
	s.listeners[ch] = struct{}{}
}

func (s *ServiceSet) Unsubscribe(ch chan *ServiceUpdate) {
	s.lisMutex.Lock()
	defer s.lisMutex.Unlock()
	delete(s.listeners, ch)
}

func (s *ServiceSet) Close() {
	// TODO: close update stream
}

type Client struct {
	client     *rpcplus.Client
	heartbeats map[string]bool
	hbMutex    sync.Mutex
}

func NewClient() (*Client, error) {
	addr := os.Getenv("DISCOVERD")
	if addr == "" {
		addr = "127.0.0.1:1111"
	}
	return NewClientUsingAddress(addr)
}

func NewClientUsingAddress(addr string) (*Client, error) {
	client, err := rpcplus.DialHTTP("tcp", addr)
	return &Client{
		client:     client,
		heartbeats: make(map[string]bool),
	}, err
}

func pickMostPublicIp() string {
	// TODO: prefer non 10.0.0.0, 172.16.0.0, and 192.168.0.0
	addrs, _ := net.InterfaceAddrs()
	var ip string
	for _, addr := range addrs {
		ip = strings.SplitN(addr.String(), "/", 2)[0]
		if !strings.Contains(ip, "::") && ip != "127.0.0.1" {
			return ip
		}
	}
	return ip
}

func (c *Client) Services(name string) (*ServiceSet, error) {
	updates := make(chan *ServiceUpdate)
	c.client.StreamGo("Agent.Subscribe", &Args{
		Name: name,
	}, updates)
	set := &ServiceSet{
		services:  make(map[string]*Service),
		filters:   make(map[string]string),
		listeners: make(map[chan *ServiceUpdate]struct{}),
	}
	<-set.bind(updates)
	return set, nil
}

func (c *Client) Register(name, port string, attributes map[string]string) error {
	return c.RegisterWithHost(name, pickMostPublicIp(), port, attributes)
}

func (c *Client) RegisterWithHost(name, host, port string, attributes map[string]string) error {
	args := &Args{
		Name:  name,
		Addr:  net.JoinHostPort(host, port),
		Attrs: attributes,
	}
	var ret struct{}
	err := c.client.Call("Agent.Register", args, &ret)
	if err != nil {
		return errors.New("discover: register failed: " + err.Error())
	}
	c.hbMutex.Lock()
	c.heartbeats[args.Addr] = true
	c.hbMutex.Unlock()
	go func() {
		var heartbeated struct{}
		for c.heartbeats[args.Addr] {
			time.Sleep(HeartbeatIntervalSecs * time.Second) // TODO: add jitter
			c.client.Call("Agent.Heartbeat", &Args{
				Name: name,
				Addr: args.Addr,
			}, &heartbeated)
		}
	}()
	return nil
}

func (c *Client) Unregister(name, port string) error {
	return c.UnregisterWithHost(name, pickMostPublicIp(), port)
}

func (c *Client) UnregisterWithHost(name, host, port string) error {
	args := &Args{
		Name: name,
		Addr: net.JoinHostPort(host, port),
	}
	var resp struct{}
	err := c.client.Call("Agent.Unregister", args, &resp)
	if err != nil {
		return errors.New("discover: unregister failed: " + err.Error())
	}
	c.hbMutex.Lock()
	delete(c.heartbeats, args.Addr)
	c.hbMutex.Unlock()
	return nil
}
