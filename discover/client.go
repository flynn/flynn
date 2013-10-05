package discover

import (
	"errors"
	"net"
	"strings"
	"sync"
	"time"

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
	serMutex  sync.RWMutex
	filMutex  sync.RWMutex
	lisMutex  sync.RWMutex
}

func (s *ServiceSet) Bind(updates chan *ServiceUpdate) {
	go func() {
		for update := range updates {
			// TODO: apply filters
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

func (s *ServiceSet) Filter(attrs map[string]string) {
	s.filters = attrs
}

// Still not sure about this API, but it's a start
func (s *ServiceSet) Subscribe(ch chan *ServiceUpdate) {
	s.lisMutex.Lock()
	defer s.lisMutex.Unlock()
	s.listeners[ch] = struct{}{}
}

func (s *ServiceSet) Unsubscribe(ch chan *ServiceUpdate) {
	// TODO: close update stream
}

type DiscoverClient struct {
	client     *rpcplus.Client
	heartbeats map[string]bool
	hbMutex    sync.RWMutex
}

func NewClient() (*DiscoverClient, error) {
	client, err := rpcplus.DialHTTP("tcp", "127.0.0.1:1111") // TODO: default, not hardcoded
	return &DiscoverClient{
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

func (c *DiscoverClient) Services(name string) *ServiceSet {
	updates := make(chan *ServiceUpdate)
	c.client.StreamGo("DiscoverAgent.Subscribe", &Args{
		Name: name,
	}, updates)
	set := &ServiceSet{
		services:  make(map[string]*Service),
		filters:   make(map[string]string),
		listeners: make(map[chan *ServiceUpdate]struct{}),
	}
	set.Bind(updates)
	return set
}

func (c *DiscoverClient) Register(name, port string, attributes map[string]string) error {
	return c.RegisterWithHost(name, pickMostPublicIp(), port, attributes)
}

func (c *DiscoverClient) RegisterWithHost(name, host, port string, attributes map[string]string) error {
	args := &Args{
		Name:  name,
		Addr:  net.JoinHostPort(host, port),
		Attrs: attributes,
	}
	var ret struct{}
	err := c.client.Call("DiscoverAgent.Register", args, &ret)
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
			c.client.Call("DiscoverAgent.Heartbeat", &Args{
				Name: name,
				Addr: args.Addr,
			}, &heartbeated)
		}
	}()
	return nil
}

func (c *DiscoverClient) Unregister(name, port string) error {
	return c.UnregisterWithHost(name, pickMostPublicIp(), port)
}

func (c *DiscoverClient) UnregisterWithHost(name, host, port string) error {
	args := &Args{
		Name: name,
		Addr: net.JoinHostPort(host, port),
	}
	var resp struct{}
	err := c.client.Call("DiscoverAgent.Unregister", args, &resp)
	if err != nil {
		return errors.New("discover: unregister failed: " + err.Error())
	}
	c.hbMutex.Lock()
	delete(c.heartbeats, args.Addr)
	c.hbMutex.Unlock()
	return nil
}
