package discover

import (
	"errors"
	"github.com/flynn/rpcplus"
	"log"
	"net"
	"strings"
	"time"
)

type Service struct {
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
	list := []*Service{}
	for _, service := range s.services {
		if service.Online {
			list = append(list, service)
		}
	}
	return list
}

func (s *ServiceSet) Offline() []*Service {
	list := []*Service{}
	for _, service := range s.services {
		if !service.Online {
			list = append(list, service)
		}
	}
	return list
}

func (s *ServiceSet) OnlineAddrs() []string {
	list := []string{}
	for _, service := range s.Online() {
		list = append(list, service.Addr)
	}
	return list
}

func (s *ServiceSet) OfflineAddrs() []string {
	list := []string{}
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

func NewClient() *DiscoverClient {
	client, err := rpcplus.DialHTTP("tcp", "127.0.0.1:1111") // TODO: default, not hardcoded
	if err != nil {
		log.Fatal("dialing:", err)
	}
	return &DiscoverClient{client: client, heartbeats: map[string]bool{}}
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

func (c *DiscoverClient) Register(name string, port string, attributes map[string]string) error {
	return c.RegisterWithHost(name, pickMostPublicIp(), port, attributes)
}

func (c *DiscoverClient) RegisterWithHost(name string, host string, port string, attributes map[string]string) error {
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
	if success {
		c.heartbeats[args.Addr] = true
		go func(name string, addr string) {
			time.Sleep(HeartbeatIntervalSecs * time.Second)
			var heartbeated bool
			for c.heartbeats[addr] {
				c.client.Call("DiscoverAgent.Heartbeat", &Args{
					Name: name,
					Addr: addr,
				}, &heartbeated)
				time.Sleep(HeartbeatIntervalSecs * time.Second) // TODO: add jitter
			}
		}(name, args.Addr)
		return nil
	} else {
		return errors.New("register failed")
	}
}

func (c *DiscoverClient) Unregister(name string, port string) error {
	return c.UnregisterWithHost(name, pickMostPublicIp(), port)
}

func (c *DiscoverClient) UnregisterWithHost(name string, host string, port string) error {
	args := &Args{
		Name: name,
		Addr: net.JoinHostPort(host, port),
	}
	var success bool // ignore success for now. nearly useless return value
	err := c.client.Call("DiscoverAgent.Unregister", args, &success)
	if err != nil {
		return err
	}
	if success {
		delete(c.heartbeats, args.Addr)
		return nil
	} else {
		return errors.New("unregister failed")
	}
}
