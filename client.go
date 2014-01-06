package discoverd

import (
	"errors"
	"net"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/flynn/discoverd/agent"
	"github.com/flynn/rpcplus"
)

type Service struct {
	Created uint
	Name    string
	Host    string
	Port    string
	Addr    string
	Attrs   map[string]string
}

type ServiceSet struct {
	sync.Mutex
	services map[string]*Service
	filters  map[string]string
	watches  map[chan *agent.ServiceUpdate]bool
	leaders  chan *Service
	call     *rpcplus.Call
	self     *Service
	SelfAddr string
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
		watches:  make(map[chan *agent.ServiceUpdate]bool),
		call:     call,
	}
}

func (s *ServiceSet) bind(updates chan *agent.ServiceUpdate) chan struct{} {
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
			s.Lock()
			if s.filters != nil && !s.matchFilters(update.Attrs) {
				s.Unlock()
				continue
			}
			if s.SelfAddr != update.Addr && update.Online {
				if _, exists := s.services[update.Addr]; !exists {
					host, port, _ := net.SplitHostPort(update.Addr)
					s.services[update.Addr] = &Service{
						Name:    update.Name,
						Addr:    update.Addr,
						Host:    host,
						Port:    port,
						Created: update.Created,
					}
				}
				s.services[update.Addr].Attrs = update.Attrs
			} else {
				if _, exists := s.services[update.Addr]; exists {
					delete(s.services, update.Addr)
				} else {
					s.Unlock()
					if s.SelfAddr == update.Addr {
						s.updateWatches(update)
					}
					continue
				}
			}
			s.Unlock()
			s.updateWatches(update)
		}
		s.closeWatches()
	}()
	return current
}

func (s *ServiceSet) updateWatches(update *agent.ServiceUpdate) {
	s.Lock()
	defer s.Unlock()
	for ch, once := range s.watches {
		ch <- update
		if once {
			delete(s.watches, ch)
		}
	}
}

func (s *ServiceSet) closeWatches() {
	s.Lock()
	defer s.Unlock()
	for ch := range s.watches {
		close(ch)
	}
}

func (s *ServiceSet) matchFilters(attrs map[string]string) bool {
	for key, value := range s.filters {
		if attrs[key] != value {
			return false
		}
	}
	return true
}

func (s *ServiceSet) Leader() chan *Service {
	if s.leaders != nil {
		return s.leaders
	}
	s.leaders = make(chan *Service)
	updates := make(chan *agent.ServiceUpdate)
	s.Watch(updates, false, false)
	go func() {
		leader := s.Services()[0]
		s.leaders <- leader
		for update := range updates {
			if update == nil {
				return
			}
			if !update.Online && update.Addr == leader.Addr {
				if len(s.Services()) == 0 && s.self != nil {
					s.leaders <- s.self
				} else {
					leader = s.Services()[0]
					if s.self != nil && leader.Created > s.self.Created {
						// self is real leader
						s.leaders <- s.self
					} else {
						s.leaders <- leader
					}
				}
			}
		}
	}()
	return s.leaders
}

type serviceByAge []*Service

func (a serviceByAge) Len() int           { return len(a) }
func (a serviceByAge) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a serviceByAge) Less(i, j int) bool { return a[i].Created < a[j].Created }

func (s *ServiceSet) Services() []*Service {
	s.Lock()
	defer s.Unlock()
	list := make([]*Service, 0, len(s.services))
	for _, service := range s.services {
		list = append(list, copyService(service))
	}
	if len(list) > 0 {
		sort.Sort(serviceByAge(list))
	}
	return list
}

func (s *ServiceSet) Addrs() []string {
	list := make([]string, 0, len(s.services))
	for _, service := range s.Services() {
		list = append(list, service.Addr)
	}
	return list
}

func (s *ServiceSet) Select(attrs map[string]string) []*Service {
	s.Lock()
	defer s.Unlock()
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
	s.Lock()
	defer s.Unlock()
	s.filters = attrs
	for key, service := range s.services {
		if !s.matchFilters(service.Attrs) {
			delete(s.services, key)
		}
	}
}

func (s *ServiceSet) Watch(ch chan *agent.ServiceUpdate, bringCurrent bool, fireOnce bool) {
	s.Lock()
	s.watches[ch] = fireOnce
	s.Unlock()
	if bringCurrent {
		go func() {
			s.Lock()
			defer s.Unlock()
			for _, service := range s.services {
				ch <- &agent.ServiceUpdate{
					Name:    service.Name,
					Addr:    service.Addr,
					Online:  true,
					Attrs:   service.Attrs,
					Created: service.Created,
				}
			}
		}()
	}
}

func (s *ServiceSet) Unwatch(ch chan *agent.ServiceUpdate) {
	s.Lock()
	defer s.Unlock()
	delete(s.watches, ch)
}

func (s *ServiceSet) Wait() chan *agent.ServiceUpdate {
	updateCh := make(chan *agent.ServiceUpdate, 1024) // buffer because of Watch bringCurrent race bug
	s.Watch(updateCh, true, true)
	return updateCh
}

func (s *ServiceSet) Close() error {
	return s.call.CloseStream()
}

type Client struct {
	sync.Mutex
	client        *rpcplus.Client
	heartbeats    map[string]chan struct{}
	expandedAddrs map[string]string
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
		client:        client,
		heartbeats:    make(map[string]chan struct{}),
		expandedAddrs: make(map[string]string),
	}, err
}

func (c *Client) ServiceSet(name string) (*ServiceSet, error) {
	updates := make(chan *agent.ServiceUpdate)
	call := c.client.StreamGo("Agent.Subscribe", &agent.Args{
		Name: name,
	}, updates)
	set := makeServiceSet(call)
	<-set.bind(updates)
	return set, nil
}

func (c *Client) Services(name string, timeout time.Duration) ([]*Service, error) {
	set, err := c.ServiceSet(name)
	if err != nil {
		return nil, err
	}
	defer set.Close()
	select {
	case <-set.Wait():
		return set.Services(), nil
	case <-time.After(time.Duration(timeout) * time.Second):
		return nil, errors.New("discover: wait timeout exceeded")
	}
}

func (c *Client) Register(name, addr string) error {
	return c.RegisterWithAttributes(name, addr, nil)
}

func (c *Client) RegisterWithSet(name, addr string, attributes map[string]string) (*ServiceSet, error) {
	err := c.RegisterWithAttributes(name, addr, attributes)
	if err != nil {
		return nil, err
	}
	set, err := c.ServiceSet(name)
	if err != nil {
		c.Unregister(name, addr)
		return nil, err
	}
	set.SelfAddr = c.expandedAddrs[addr]
	_, exists := set.services[set.SelfAddr]
	if !exists {
		update := <-set.Wait()
		for update.Addr != set.SelfAddr {
			update = <-set.Wait()
		}
	}
	set.Lock()
	set.self = set.services[set.SelfAddr]
	delete(set.services, set.SelfAddr)
	set.Unlock()
	return set, nil
}

func (c *Client) RegisterAndStandby(name, addr string, attributes map[string]string) (chan *Service, error) {
	set, err := c.RegisterWithSet(name, addr, attributes)
	if err != nil {
		return nil, err
	}
	standbyCh := make(chan *Service)
	go func() {
		for leader := range set.Leader() {
			if leader.Addr == set.SelfAddr {
				set.Close()
				standbyCh <- leader
				return
			}
		}
	}()
	return standbyCh, nil
}

func (c *Client) RegisterWithAttributes(name, addr string, attributes map[string]string) error {
	args := &agent.Args{
		Name:  name,
		Addr:  addr,
		Attrs: attributes,
	}
	var ret string
	err := c.client.Call("Agent.Register", args, &ret)
	if err != nil {
		return errors.New("discover: register failed: " + err.Error())
	}
	done := make(chan struct{})
	c.Lock()
	c.heartbeats[args.Addr] = done
	c.expandedAddrs[args.Addr] = ret
	c.Unlock()
	go func() {
		ticker := time.NewTicker(agent.HeartbeatIntervalSecs * time.Second) // TODO: add jitter
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// TODO: log error here
				c.client.Call("Agent.Heartbeat", &agent.Args{
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

func (c *Client) Unregister(name, addr string) error {
	args := &agent.Args{
		Name: name,
		Addr: addr,
	}
	c.Lock()
	close(c.heartbeats[args.Addr])
	delete(c.heartbeats, args.Addr)
	c.Unlock()
	err := c.client.Call("Agent.Unregister", args, &struct{}{})
	if err != nil {
		return errors.New("discover: unregister failed: " + err.Error())
	}
	return nil
}
