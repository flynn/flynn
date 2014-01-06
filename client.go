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
	l        sync.Mutex
	services map[string]*Service
	filters  map[string]string
	watches  map[chan *agent.ServiceUpdate]bool
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
			s.l.Lock()
			if s.filters != nil && !s.matchFilters(update.Attrs) {
				s.l.Unlock()
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
					if s.SelfAddr == update.Addr {
						s.l.Unlock()
						s.updateWatches(update)
					} else {
						s.l.Unlock()
					}
					continue
				}
			}
			s.l.Unlock()
			s.updateWatches(update)
		}
		s.closeWatches()
	}()
	return current
}

func (s *ServiceSet) updateWatches(update *agent.ServiceUpdate) {
	s.l.Lock()
	watches := make(map[chan *agent.ServiceUpdate]bool, len(s.watches))
	for k, v := range s.watches {
		watches[k] = v
	}
	s.l.Unlock()
	for ch, once := range watches {
		select {
		case ch <- update:
		case <-time.After(time.Millisecond):
		}
		if once {
			close(ch)
			s.l.Lock()
			delete(s.watches, ch)
			s.l.Unlock()
		}
	}
}

func (s *ServiceSet) closeWatches() {
	s.l.Lock()
	watches := make(map[chan *agent.ServiceUpdate]bool, len(s.watches))
	for k, v := range s.watches {
		watches[k] = v
	}
	s.l.Unlock()
	for ch := range watches {
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

func (s *ServiceSet) Leader() *Service {
	services := s.Services()
	if len(services) > 0 {
		if s.self != nil && services[0].Created > s.self.Created {
			return s.self
		}
		return services[0]
	}
	if s.self != nil {
		return s.self
	}
	return nil
}

func (s *ServiceSet) Leaders() chan *Service {
	leaders := make(chan *Service)
	updates := s.Watch(false, false)
	go func() {
		leader := s.Leader()
		leaders <- leader
		for update := range updates {
			if !update.Online && update.Addr == leader.Addr {
				leader = s.Leader()
				leaders <- leader
			}
		}
	}()
	return leaders
}

type serviceByAge []*Service

func (a serviceByAge) Len() int           { return len(a) }
func (a serviceByAge) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a serviceByAge) Less(i, j int) bool { return a[i].Created < a[j].Created }

func (s *ServiceSet) Services() []*Service {
	s.l.Lock()
	defer s.l.Unlock()
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
	s.l.Lock()
	defer s.l.Unlock()
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
	s.l.Lock()
	defer s.l.Unlock()
	s.filters = attrs
	for key, service := range s.services {
		if !s.matchFilters(service.Attrs) {
			delete(s.services, key)
		}
	}
}

func (s *ServiceSet) Watch(bringCurrent bool, fireOnce bool) chan *agent.ServiceUpdate {
	s.l.Lock()
	defer s.l.Unlock()
	var updates chan *agent.ServiceUpdate
	if bringCurrent {
		updates = make(chan *agent.ServiceUpdate, len(s.services))
		for _, service := range s.services {
			updates <- &agent.ServiceUpdate{
				Name:    service.Name,
				Addr:    service.Addr,
				Online:  true,
				Attrs:   service.Attrs,
				Created: service.Created,
			}
		}
	} else {
		updates = make(chan *agent.ServiceUpdate)
	}
	s.watches[updates] = fireOnce
	return updates
}

func (s *ServiceSet) Unwatch(ch chan *agent.ServiceUpdate) {
	s.l.Lock()
	defer s.l.Unlock()
	close(ch)
	delete(s.watches, ch)
}

func (s *ServiceSet) Close() error {
	return s.call.CloseStream()
}

type Client struct {
	l             sync.Mutex
	client        *rpcplus.Client
	heartbeats    map[string]chan struct{}
	expandedAddrs map[string]string
	names         map[string]string
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
		names:         make(map[string]string),
	}, err
}

func (c *Client) NewServiceSet(name string) (*ServiceSet, error) {
	updates := make(chan *agent.ServiceUpdate)
	call := c.client.StreamGo("Agent.Subscribe", &agent.Args{
		Name: name,
	}, updates)
	set := makeServiceSet(call)
	<-set.bind(updates)
	return set, nil
}

func (c *Client) Services(name string, timeout time.Duration) ([]*Service, error) {
	set, err := c.NewServiceSet(name)
	if err != nil {
		return nil, err
	}
	defer set.Close()
	select {
	case <-set.Watch(true, true):
		return set.Services(), nil
	case <-time.After(timeout):
		return nil, errors.New("discover: timeout exceeded")
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
	set, err := c.NewServiceSet(name)
	if err != nil {
		c.Unregister(name, addr)
		return nil, err
	}
	set.l.Lock()
	c.l.Lock()
	set.SelfAddr = c.expandedAddrs[addr]
	c.l.Unlock()
	set.l.Unlock()
	updates := set.Watch(true, false)
	for update := range updates {
		if update.Addr == set.SelfAddr {
			set.Unwatch(updates)
			break
		}
	}
	set.l.Lock()
	set.self = set.services[set.SelfAddr]
	delete(set.services, set.SelfAddr)
	set.l.Unlock()
	return set, nil
}

func (c *Client) RegisterAndStandby(name, addr string, attributes map[string]string) (chan *Service, error) {
	set, err := c.RegisterWithSet(name, addr, attributes)
	if err != nil {
		return nil, err
	}
	standbyCh := make(chan *Service)
	go func() {
		for leader := range set.Leaders() {
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
	c.l.Lock()
	c.heartbeats[args.Addr] = done
	c.expandedAddrs[args.Addr] = ret
	c.names[args.Addr] = name
	c.l.Unlock()
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
	c.l.Lock()
	close(c.heartbeats[args.Addr])
	delete(c.heartbeats, args.Addr)
	c.l.Unlock()
	err := c.client.Call("Agent.Unregister", args, &struct{}{})
	if err != nil {
		return errors.New("discover: unregister failed: " + err.Error())
	}
	return nil
}

func (c *Client) UnregisterAll() error {
	c.l.Lock()
	addrs := make([]string, 0, len(c.heartbeats))
	names := make([]string, 0, len(c.heartbeats))
	for addr, _ := range c.heartbeats {
		addrs = append(addrs, addr)
		names = append(names, c.names[addr])
	}
	c.l.Unlock()
	for i := range addrs {
		err := c.Unregister(names[i], addrs[i])
		if err != nil {
			return err
		}
	}
	return nil
}

var defaultClient *Client

func Connect(addr string) (err error) {
	if addr == "" {
		defaultClient, err = NewClient()
		return
	}
	defaultClient, err = NewClientUsingAddress(addr)
	return
}

func ensureDefaultConnected() error {
	if defaultClient == nil {
		return Connect("")
	}
	return nil
}

func NewServiceSet(name string) (*ServiceSet, error) {
	if err := ensureDefaultConnected(); err != nil {
		return nil, err
	}
	return defaultClient.NewServiceSet(name)
}

func Services(name string, timeout time.Duration) ([]*Service, error) {
	if err := ensureDefaultConnected(); err != nil {
		return nil, err
	}
	return defaultClient.Services(name, timeout)
}

func Register(name, addr string) error {
	if err := ensureDefaultConnected(); err != nil {
		return err
	}
	return defaultClient.Register(name, addr)
}

func RegisterWithSet(name, addr string, attributes map[string]string) (*ServiceSet, error) {
	if err := ensureDefaultConnected(); err != nil {
		return nil, err
	}
	return defaultClient.RegisterWithSet(name, addr, attributes)
}

func RegisterAndStandby(name, addr string, attributes map[string]string) (chan *Service, error) {
	if err := ensureDefaultConnected(); err != nil {
		return nil, err
	}
	return defaultClient.RegisterAndStandby(name, addr, attributes)
}

func RegisterWithAttributes(name, addr string, attributes map[string]string) error {
	if err := ensureDefaultConnected(); err != nil {
		return err
	}
	return defaultClient.RegisterWithAttributes(name, addr, attributes)
}

func Unregister(name, addr string) error {
	if err := ensureDefaultConnected(); err != nil {
		return err
	}
	return defaultClient.Unregister(name, addr)
}

func UnregisterAll() error {
	if err := ensureDefaultConnected(); err != nil {
		return err
	}
	return defaultClient.UnregisterAll()
}
