// Client library for discoverd that gives you service discovery and registration, as well as basic
// leader election coordination. It provides a high-level API around the lower-level service API of
// the discoverd service.
package discoverd

import (
	"errors"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flynn/discoverd/agent"
	"github.com/flynn/rpcplus"
)

// This is a reasonable default value to be used for the timeout in the Services method.
const DefaultTimeout = time.Second

// This is how we model a service. It's simply a named address with optional attributes.
// It also has a field to determine age, which is used for leader election.
type Service struct {
	Created uint
	Name    string
	Host    string
	Port    string
	Addr    string
	Attrs   map[string]string
}

type serviceSet struct {
	l        sync.Mutex
	services map[string]*Service
	filters  map[string]string
	watches  map[chan *agent.ServiceUpdate]bool
	call     *rpcplus.Call
	self     *Service
	selfAddr string
}

// A ServiceSet is long-running query of services, giving you a real-time representation of a
// service cluster. Using the same ServiceSet across services/processes gives you a consistent
// collection of services that leader election can be coordinated with.
type ServiceSet interface {
	SelfAddr() string

	// Leader returns the current leader for a ServiceSet. It's calculated by choosing the oldest
	// service in the set. This "lockless" approach means for any consistent set (same service, same
	// filters) there is always an agreed upon leader.
	Leader() *Service

	// Leaders returns a channel that will first produce the current leader service, then any following
	// leader service as the leader of the set changes. Every call to Leaders produces a new watch on
	// the set, and once you get a channel from Leaders, you *must* always be receiving until the
	// ServiceSet is closed. A nil value will be sent if there are no members of the set.
	Leaders() chan *Service

	// Services returns an array of Service objects in the set, sorted by age. This means that most
	// of the time, the first element is the leader. However, in cases where the ServiceSet was
	// created by RegisterWithSet, the registered service will not be included in this list, so you
	// should rely on Leader/Leaders to get the leader.
	Services() []*Service

	// Addrs returns an array of strings representing the addresses of the services.
	Addrs() []string

	// Select will return an array of services with matching attributes to the provided map argument.
	// Unlike the Services method, Select is not ordered.
	Select(attrs map[string]string) []*Service

	// Filter will set the filter map for a ServiceSet. A filter will limit services that show up in
	// the set to only the ones with matching attributes. Any services in the set that don't match
	// when Filter is called will be removed from the ServiceSet.
	Filter(attrs map[string]string)

	// Watch gives you a channel of updates that are made to the ServiceSet. Once you create a watch,
	// you must always be receiving on it until the ServiceSet is closed or you call Unwatch with the
	// channel.
	//
	// The bringCurrent argument will produce a channel that will have buffered in it updates
	// representing the current services in the set. Otherwise, the channel returned will be
	// unbuffered. The fireOnce argument allows you to produce a watch that will only be used once
	// and then immediately removed from watches.
	Watch(bringCurrent bool, fireOnce bool) chan *agent.ServiceUpdate

	// Unwatch removes a channel from the watch list of the ServiceSet. It will also close the channel.
	Unwatch(chan *agent.ServiceUpdate)

	// Close will stop a ServiceSet from being updated.
	Close() error
}

func copyService(service *Service) *Service {
	s := *service
	s.Attrs = make(map[string]string, len(service.Attrs))
	for k, v := range service.Attrs {
		s.Attrs[k] = v
	}
	return &s
}

func makeServiceSet(call *rpcplus.Call) *serviceSet {
	return &serviceSet{
		services: make(map[string]*Service),
		filters:  make(map[string]string),
		watches:  make(map[chan *agent.ServiceUpdate]bool),
		call:     call,
	}
}

func (s *serviceSet) SelfAddr() string {
	return s.selfAddr
}

func (s *serviceSet) bind(updates chan *agent.ServiceUpdate) chan struct{} {
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
			if s.selfAddr != update.Addr && update.Online {
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
					if s.selfAddr == update.Addr {
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

func (s *serviceSet) updateWatches(update *agent.ServiceUpdate) {
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

func (s *serviceSet) closeWatches() {
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

func (s *serviceSet) matchFilters(attrs map[string]string) bool {
	for key, value := range s.filters {
		if attrs[key] != value {
			return false
		}
	}
	return true
}

func (s *serviceSet) Leader() *Service {
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

func (s *serviceSet) Leaders() chan *Service {
	leaders := make(chan *Service)
	updates := s.Watch(false, false)
	go func() {
		leader := s.Leader()
		leaders <- leader
		for update := range updates {
			if (!update.Online && update.Addr == leader.Addr) || (update.Online && leader == nil) {
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

func (s *serviceSet) Services() []*Service {
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

func (s *serviceSet) Addrs() []string {
	list := make([]string, 0, len(s.services))
	for _, service := range s.Services() {
		list = append(list, service.Addr)
	}
	return list
}

func (s *serviceSet) Select(attrs map[string]string) []*Service {
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

func (s *serviceSet) Filter(attrs map[string]string) {
	s.l.Lock()
	defer s.l.Unlock()
	s.filters = attrs
	for key, service := range s.services {
		if !s.matchFilters(service.Attrs) {
			delete(s.services, key)
		}
	}
}

func (s *serviceSet) Watch(bringCurrent bool, fireOnce bool) chan *agent.ServiceUpdate {
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

func (s *serviceSet) Unwatch(ch chan *agent.ServiceUpdate) {
	go func() {
		// drain channel to prevent deadlock
		for _ = range ch {
		}
	}()
	s.l.Lock()
	defer s.l.Unlock()
	close(ch)
	delete(s.watches, ch)
}

func (s *serviceSet) Close() error {
	return s.call.CloseStream()
}

// A Client maintains the RPC client connection and any active registered services, making sure
// they receive their heartbeat calls.
type Client struct {
	l             sync.Mutex
	client        *rpcplus.Client
	heartbeats    map[string]chan struct{}
	expandedAddrs map[string]string
	names         map[string]string
}

// By default, NewClient will produce a client that will connect to the default address for
// discoverd, which is 127.0.0.1:1111, or whatever the value of the environment variable DISCOVERD.
func NewClient() (*Client, error) {
	addr := os.Getenv("DISCOVERD")
	if addr == "" {
		addr = "127.0.0.1:1111"
	} else {
		addr = strings.TrimPrefix(addr, "tcp://")
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

func (c *Client) newServiceSet(name string) (*serviceSet, error) {
	updates := make(chan *agent.ServiceUpdate)
	call := c.client.StreamGo("Agent.Subscribe", &agent.Args{
		Name: name,
	}, updates)
	set := makeServiceSet(call)
	<-set.bind(updates)
	return set, nil
}

// NewServiceSet will produce a ServiceSet for a given service name.
func (c *Client) NewServiceSet(name string) (ServiceSet, error) {
	return c.newServiceSet(name)
}

// Services returns an array of Service objects of a given name. It provides a much easier way to
// get at a snapshot of services than using ServiceSet. It will also block until at least one
// service is available, or until the timeout specified is exceeded.
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

// Register will announce a service as available and online at the address specified. If you only
// specify a port as the address, discoverd may expand it to a full host and port based on the
// external IP of the discoverd agent.
func (c *Client) Register(name, addr string) error {
	return c.RegisterWithAttributes(name, addr, nil)
}

// RegisterWithSet combines service registration with NewServiceSet for the same service, but will
// not include the registered service in the ServiceSet. If you have a cluster of services that all
// connect to a leader, this is especially useful as you don't have to worry about not connecting
// to yourself. When using Leader or Leaders with RegisterWithSet, you may still get a Service
// representing the service registered, in case this service does become leader. In that case, you
// can use the SelfAddr property of ServiceSet to compare to the Service returned by Leader.
func (c *Client) RegisterWithSet(name, addr string, attributes map[string]string) (ServiceSet, error) {
	err := c.RegisterWithAttributes(name, addr, attributes)
	if err != nil {
		return nil, err
	}
	set, err := c.newServiceSet(name)
	if err != nil {
		c.Unregister(name, addr)
		return nil, err
	}
	set.l.Lock()
	c.l.Lock()
	set.selfAddr = c.expandedAddrs[addr]
	c.l.Unlock()
	set.l.Unlock()
	updates := set.Watch(true, false)
	for update := range updates {
		if update.Addr == set.selfAddr {
			set.Unwatch(updates)
			break
		}
	}
	set.l.Lock()
	set.self = set.services[set.selfAddr]
	delete(set.services, set.selfAddr)
	set.l.Unlock()
	return set, nil
}

// RegisterAndStandby will register a service and returns a channel that will only be fired
// when this service is or becomes leader. This can be used to implement a standby mode, where your
// service doesn't actually start serving until it becomes a leader. You can also use this for more
// standard leader election upgrades by placing the receive for this channel in a goroutine.
func (c *Client) RegisterAndStandby(name, addr string, attributes map[string]string) (chan *Service, error) {
	set, err := c.RegisterWithSet(name, addr, attributes)
	if err != nil {
		return nil, err
	}
	standbyCh := make(chan *Service)
	go func() {
		for leader := range set.Leaders() {
			if leader.Addr == set.SelfAddr() {
				set.Close()
				standbyCh <- leader
				return
			}
		}
	}()
	return standbyCh, nil
}

// RegisterWithAttributes registers a service to be discovered, setting the attribtues specified, however,
// attributes are optional so the value can be nil. If you need to change attributes, you just reregister.
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

// ErrUnknownRegistration is returned by Unregister when no registration is found.
var ErrUnknownRegistration = errors.New("discover: unknown registration")

// Unregister will explicitly unregister a service and as such it will stop any heartbeats
// being sent from this client.
func (c *Client) Unregister(name, addr string) error {
	args := &agent.Args{
		Name: name,
		Addr: addr,
	}
	c.l.Lock()
	ch, ok := c.heartbeats[args.Addr]
	if !ok {
		c.l.Unlock()
		return ErrUnknownRegistration
	}
	close(ch)
	delete(c.heartbeats, args.Addr)
	c.l.Unlock()
	err := c.client.Call("Agent.Unregister", args, &struct{}{})
	if err != nil {
		return errors.New("discover: unregister failed: " + err.Error())
	}
	return nil
}

// UnregisterAll will call Unregister on all services that have been registered with this client.
func (c *Client) UnregisterAll() error {
	c.l.Lock()
	addrs := make([]string, 0, len(c.heartbeats))
	names := make([]string, 0, len(c.heartbeats))
	for addr := range c.heartbeats {
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

// The DefaultClient is used for all the top-level functions. You don't have to create it, but
// you can change the address it uses by calling Connect.
var DefaultClient *Client
var defaultEnsureLock = &sync.Mutex{}

// Normally you don't have to call Connect because it's called implicitly by the top-level
// functions. However, you can call connect if you need to connect to a specific address other
// than the default or value in the DISCOVERD env var. Calling Connect will replace any existing
// client value for DefaultClient, so be sure to call it early if you intend to use it.
func Connect(addr string) (err error) {
	if addr == "" {
		DefaultClient, err = NewClient()
		return
	}
	DefaultClient, err = NewClientUsingAddress(addr)
	return
}

func ensureDefaultConnected() error {
	defaultEnsureLock.Lock()
	defer defaultEnsureLock.Unlock()
	if DefaultClient == nil {
		return Connect("")
	}
	return nil
}

// NewServiceSet will produce a ServiceSet for a given service name.
func NewServiceSet(name string) (ServiceSet, error) {
	if err := ensureDefaultConnected(); err != nil {
		return nil, err
	}
	return DefaultClient.NewServiceSet(name)
}

// Services returns an array of Service objects of a given name. It provides a much easier way to
// get at a snapshot of services than using ServiceSet. It will also block until at least one
// service is available, or until the timeout specified is exceeded.
func Services(name string, timeout time.Duration) ([]*Service, error) {
	if err := ensureDefaultConnected(); err != nil {
		return nil, err
	}
	return DefaultClient.Services(name, timeout)
}

// Register will announce a service as available and online at the address specified. If you only
// specify a port as the address, discoverd may expand it to a full host and port based on the
// external IP of the discoverd agent.
func Register(name, addr string) error {
	if err := ensureDefaultConnected(); err != nil {
		return err
	}
	return DefaultClient.Register(name, addr)
}

// RegisterWithSet combines service registration with NewServiceSet for the same service, but will
// not include the registered service in the ServiceSet. If you have a cluster of services that all
// connect to a leader, this is especially useful as you don't have to worry about not connecting
// to yourself. When using Leader or Leaders with RegisterWithSet, you may still get a Service
// representing the service registered, in case this service does become leader. In that case, you
// can use the SelfAddr property of ServiceSet to compare to the Service returned by Leader.
func RegisterWithSet(name, addr string, attributes map[string]string) (ServiceSet, error) {
	if err := ensureDefaultConnected(); err != nil {
		return nil, err
	}
	return DefaultClient.RegisterWithSet(name, addr, attributes)
}

// RegisterAndStandby will register a service and returns a channel that will only be fired
// when this service is or becomes leader. This can be used to implement a standby mode, where your
// service doesn't actually start serving until it becomes a leader. You can also use this for more
// standard leader election upgrades by placing the receive for this channel in a goroutine.
func RegisterAndStandby(name, addr string, attributes map[string]string) (chan *Service, error) {
	if err := ensureDefaultConnected(); err != nil {
		return nil, err
	}
	return DefaultClient.RegisterAndStandby(name, addr, attributes)
}

// RegisterWithAttributes registers a service to be discovered, setting the attribtues specified, however,
// attributes are optional so the value can be nil. If you need to change attributes, you just reregister.
func RegisterWithAttributes(name, addr string, attributes map[string]string) error {
	if err := ensureDefaultConnected(); err != nil {
		return err
	}
	return DefaultClient.RegisterWithAttributes(name, addr, attributes)
}

// Unregister will explicitly unregister a service and as such it will stop any heartbeats
// being sent from this client.
func Unregister(name, addr string) error {
	if err := ensureDefaultConnected(); err != nil {
		return err
	}
	return DefaultClient.Unregister(name, addr)
}

// UnregisterAll will call Unregister on all services that have been registered with this client.
func UnregisterAll() error {
	if err := ensureDefaultConnected(); err != nil {
		return err
	}
	return DefaultClient.UnregisterAll()
}
