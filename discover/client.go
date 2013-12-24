package discover

import (
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/flynn/rpcplus"
)

var WaitTimeoutSecs = 10

type Service struct {
	Created int
	Name    string
	Host    string
	Port    string
	Addr    string
	Attrs   map[string]string
}

type ServiceSet struct {
	services   map[string]*Service
	filters    map[string]string
	watches    map[chan *ServiceUpdate]struct{}
	serMutex   sync.Mutex
	filMutex   sync.Mutex
	watchMutex sync.Mutex
	call       *rpcplus.Call
}

func copyService(service *Service) *Service {
	s := *service
	s.Attrs = make(map[string]string, len(service.Attrs))
	for k, v := range service.Attrs {
		s.Attrs[k] = v
	}
	return &s
}

func makeServiceSet(call *rpcplus.Call) *ServiceSet {
	return &ServiceSet{
		services: make(map[string]*Service),
		filters:  make(map[string]string),
		watches:  make(map[chan *ServiceUpdate]struct{}),
		call:     call,
	}
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
			if update.Online {
				if _, exists := s.services[update.Addr]; !exists {
					host, port, _ := net.SplitHostPort(update.Addr)
					s.services[update.Addr] = &Service{
						Name: update.Name,
						Addr: update.Addr,
						Host: host,
						Port: port,
					}
				}
				s.services[update.Addr].Attrs = update.Attrs
			} else {
				if _, exists := s.services[update.Addr]; exists {
					delete(s.services, update.Addr)
				} else {
					s.serMutex.Unlock()
					continue
				}
			}
			s.serMutex.Unlock()

			s.updateWatches(update)
		}
		//s.closeWatches()
	}()
	return current
}

func (s *ServiceSet) updateWatches(update *ServiceUpdate) {
	s.watchMutex.Lock()
	defer s.watchMutex.Unlock()
	for ch := range s.watches {
		ch <- update
	}
}

func (s *ServiceSet) closeWatches() {
	s.watchMutex.Lock()
	defer s.watchMutex.Unlock()
	for ch := range s.watches {
		close(ch)
	}
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

// deprecated
func (s *ServiceSet) Online() []*Service {
	return s.Services()
}

func (s *ServiceSet) Services() []*Service {
	s.serMutex.Lock()
	defer s.serMutex.Unlock()
	list := make([]*Service, 0, len(s.services))
	for _, service := range s.services {
		list = append(list, copyService(service))
	}
	return list
}

// deprecated
func (s *ServiceSet) OnlineAddrs() []string {
	return s.Addrs()
}

func (s *ServiceSet) Addrs() []string {
	list := make([]string, 0, len(s.services))
	for _, service := range s.Services() {
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

func (s *ServiceSet) Watch(ch chan *ServiceUpdate, bringCurrent bool) {
	s.watchMutex.Lock()
	defer s.watchMutex.Unlock()
	s.watches[ch] = struct{}{}
	if bringCurrent {
		go func() {
			s.serMutex.Lock()
			defer s.serMutex.Unlock()
			for _, service := range s.services {
				ch <- &ServiceUpdate{
					Name:   service.Name,
					Addr:   service.Addr,
					Online: true,
					Attrs:  service.Attrs,
				}
			}
		}()
	}
}

func (s *ServiceSet) Unwatch(ch chan *ServiceUpdate) {
	s.watchMutex.Lock()
	defer s.watchMutex.Unlock()
	delete(s.watches, ch)
}

func (s *ServiceSet) Wait() (*ServiceUpdate, error) {
	updateCh := make(chan *ServiceUpdate, 1)
	s.Watch(updateCh, true)
	defer s.Unwatch(updateCh)
	timeout := make(chan struct{})
	go func() {
		time.Sleep(time.Duration(WaitTimeoutSecs) * time.Second)
		timeout <- struct{}{}
	}()
	select {
	case update := <-updateCh:
		return update, nil
	case <-timeout:
		return nil, errors.New("discover: wait timeout exceeded")
	}
}

func (s *ServiceSet) Close() error {
	err := s.call.CloseStream()
	if err != nil {
		return err
	}
	return nil
}

type Client struct {
	client     *rpcplus.Client
	heartbeats map[string]chan struct{}
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
		heartbeats: make(map[string]chan struct{}),
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

func (c *Client) ServiceSet(name string) (*ServiceSet, error) {
	updates := make(chan *ServiceUpdate)
	call := c.client.StreamGo("Agent.Subscribe", &Args{
		Name: name,
	}, updates)
	set := makeServiceSet(call)
	<-set.bind(updates)
	return set, nil
}

func (c *Client) Services(name string) ([]*Service, error) {
	set, err := c.ServiceSet(name)
	if err != nil {
		return nil, err
	}
	if len(set.Services()) == 0 {
		_, err = set.Wait()
		if err != nil {
			return nil, err
		}
	}
	set.Close()
	return set.Services(), nil

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
	done := make(chan struct{})
	c.hbMutex.Lock()
	c.heartbeats[args.Addr] = done
	c.hbMutex.Unlock()
	go func() {
		ticker := time.NewTicker(HeartbeatIntervalSecs * time.Second) // TODO: add jitter
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// TODO: log error here
				c.client.Call("Agent.Heartbeat", &Args{
					Name: name,
					Addr: args.Addr,
				}, &struct{}{})
			case <-done:
				return
			}
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
	c.hbMutex.Lock()
	close(c.heartbeats[args.Addr])
	delete(c.heartbeats, args.Addr)
	c.hbMutex.Unlock()
	err := c.client.Call("Agent.Unregister", args, &struct{}{})
	if err != nil {
		return errors.New("discover: unregister failed: " + err.Error())
	}
	return nil
}
