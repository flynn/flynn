package discover

import (
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/flynn/rpcplus"
)

// TODO Populate the id's after understanding the backends mechanism.
type Service struct {
	Id     int
	Name   string
	Host   string
	Port   string
	Addr   string
	Attrs  map[string]string
	Online bool
}

// TODO Use mutex appropriately and add a test for it.
type ServiceSet struct {
	services  map[string]*Service
	filters   map[string]string // Whats a filter?
	listeners map[chan *ServiceUpdate]struct{}
	serMutex sync.RWMutex
	filMutex sync.RWMutex
	lisMutex sync.RWMutex
}

func (s *ServiceSet) Bind(updates chan *ServiceUpdate) {
	go func() {
		for update := range updates {
			// TODO: apply filters
			_, exists := s.services[update.Addr]
			if !exists {
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
			if s.listeners != nil {
				for ch := range s.listeners {
					ch <- update
				}
			}
		}
	}()
}

func (s *ServiceSet) Online() []*Service {
	list := make([]*Service, 0, len(s.services))
	for _, service := range s.services {
		if service.Online {
			list = append(list, service)
		}
	}
	return list
}

func (s *ServiceSet) Offline() []*Service {
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
func (s *ServiceSet) Listen(ch chan *ServiceUpdate) {
	s.listeners[ch] = struct{}{}
}

func (s *ServiceSet) Unsubscribe() {
	// noop for now. TODO: close update stream
}

type DiscoverClient struct {
	client     *rpcplus.Client
	heartbeats map[string]bool
}

func NewClient() (*DiscoverClient, error) {
	client, err := rpcplus.DialHTTP("tcp", "127.0.0.1:1111") // TODO: default, not hardcoded
	return &DiscoverClient{
		client : client,
		heartbeats : make(map[string]bool),
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
	updates := make(chan *ServiceUpdate, 10)
	c.client.StreamGo("DiscoverAgent.Subscribe", &Args{
		Name: name,
	}, updates)
	set := &ServiceSet{
		services: map[string]*Service{},
		filters:  map[string]string{},
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
	var success bool
	err := c.client.Call("DiscoverAgent.Register", args, &success)
	if err != nil {
		return err
	}
	if !success {
		return errors.New("discover : register failed")
	} else {
		c.heartbeats[args.Addr] = true
		go func() {
			time.Sleep(HeartbeatIntervalSecs * time.Second)
			var heartbeated bool
			for c.heartbeats[args.Addr] {
				c.client.Call("DiscoverAgent.Heartbeat", &Args{
					Name: name,
					Addr: args.Addr,
				}, &heartbeated)
				time.Sleep(HeartbeatIntervalSecs * time.Second) // TODO: add jitter
			}
		}()
		return nil
	}
}

func (c *DiscoverClient) Unregister(name, port string) error {
	return c.UnregisterWithHost(name, pickMostPublicIp(), port)
}

func (c *DiscoverClient) UnregisterWithHost(name, host, port string) error {
	args := &Args{
		Name: name,
		Addr: net.JoinHostPort(host, port),
	}
	var success bool // ignore success for now. nearly useless return value
	err := c.client.Call("DiscoverAgent.Unregister", args, &success)
	if err != nil {
		return err
	}
	if !success {
		return errors.New("discover : unregister failed")
	} else {
		delete(c.heartbeats, args.Addr)
		return nil
	}
}
