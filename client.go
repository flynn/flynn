// Client library for discoverd that gives you service discovery and registration, as well as basic
// leader election coordination. It provides a high-level API around the lower-level service API of
// the discoverd service.
package discoverd

import (
	"errors"
	"fmt"
	"io"
	"log"
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
	l         sync.Mutex
	services  map[string]*Service
	filters   map[string]string
	watches   map[chan *agent.ServiceUpdate]struct{}
	call      *rpcplus.Call
	self      *Service
	selfAddr  string
	closed    bool
	closedMtx sync.RWMutex
	c         *Client
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
	// unbuffered.
	Watch(bringCurrent bool) chan *agent.ServiceUpdate

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

func makeServiceSet(c *Client) *serviceSet {
	return &serviceSet{
		services: make(map[string]*Service),
		filters:  make(map[string]string),
		watches:  make(map[chan *agent.ServiceUpdate]struct{}),
		c:        c,
	}
}

func (s *serviceSet) SelfAddr() string {
	return s.selfAddr
}

func (s *serviceSet) bind(name string) chan error {
	// current is an event when enough service updates have been
	// received to bring us to "current" state (when subscribed)
	current := make(chan error)
	go func() {
		isCurrent := false
		// known is set after reconnecting so we can compare it with the current state
		// returned by discoverd to determine if any services have gone offline whilst
		// we were disconnected
		var known map[string]*Service
		// services is used to store updates so that when we are receiving the state
		// from discoverd after reconnecting, s.services continues to contain the services
		// prior to disconnection until we reach current state
		services := make(map[string]*Service)
		for {
			if len(s.services) == 0 {
				// we may as well keep s.services in sync if it's empty
				s.setServices(services)
			}
			updates := make(chan *agent.ServiceUpdate)
			client, call := s.c.streamGo("Agent.Subscribe", &agent.Args{
				Name: name,
			}, updates)
			s.call = call
			for update := range updates {
				if update.Addr == "" && update.Name == "" {
					if isCurrent {
						// check if any known services have gone offline
						for _, service := range known {
							if _, exists := services[service.Addr]; !exists {
								s.updateWatches(&agent.ServiceUpdate{
									Name:    service.Name,
									Addr:    service.Addr,
									Online:  false,
									Attrs:   service.Attrs,
									Created: service.Created,
								})
							}
						}
						s.setServices(services)
					} else {
						close(current)
						isCurrent = true
					}
					continue
				}
				s.l.Lock()
				if s.filters != nil && !s.matchFilters(update.Attrs) {
					s.l.Unlock()
					continue
				}
				// add a new service if the address is unrecognized
				// and the address is online
				if s.selfAddr != update.Addr && update.Online {
					if _, exists := services[update.Addr]; !exists {
						host, port, _ := net.SplitHostPort(update.Addr)
						services[update.Addr] = &Service{
							Name:    update.Name,
							Addr:    update.Addr,
							Host:    host,
							Port:    port,
							Created: update.Created,
						}
					}
					services[update.Addr].Attrs = update.Attrs
				} else {
					if _, exists := services[update.Addr]; exists {
						delete(services, update.Addr)
					} else {
						s.l.Unlock()
						if s.selfAddr == update.Addr {
							s.updateWatches(update)
						}
						continue
					}
				}
				s.l.Unlock()
				s.updateWatches(update)
			}
			if (s.call.Error == rpcplus.ErrShutdown || s.call.Error == io.ErrUnexpectedEOF) && !s.isClosed() {
				if !isCurrent {
					current <- ErrDisconnected
					go s.c.reconnect(client)
				} else {
					err := s.c.reconnect(client)
					if err == nil {
						known = services
						services = make(map[string]*Service)
						continue
					}
					log.Printf("discover: failed to reconnect: %s", err)
				}
			}
			break
		}
		s.closeWatches()
	}()
	return current
}

func (s *serviceSet) updateWatches(update *agent.ServiceUpdate) {
	s.l.Lock()
	watches := make(map[chan *agent.ServiceUpdate]struct{}, len(s.watches))
	for k, v := range s.watches {
		watches[k] = v
	}
	s.l.Unlock()
	for ch, _ := range watches {
		// TODO: figure out better head blocking for
		// slow watchers.
		ch <- update
	}
}

func (s *serviceSet) closeWatches() {
	s.l.Lock()
	watches := make(map[chan *agent.ServiceUpdate]struct{}, len(s.watches))
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
	updates := s.Watch(false)
	go func() {
		leader := s.Leader()
		leaders <- leader
		for update := range updates {
			if (!update.Online && leader != nil && update.Addr == leader.Addr) || (update.Online && leader == nil) {
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

func (s *serviceSet) setServices(services map[string]*Service) {
	s.l.Lock()
	defer s.l.Unlock()
	s.services = services
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

func (s *serviceSet) Watch(bringCurrent bool) chan *agent.ServiceUpdate {
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
	s.watches[updates] = struct{}{}
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
	s.setClosed()
	return s.call.CloseStream()
}

func (s *serviceSet) isClosed() bool {
	s.closedMtx.RLock()
	defer s.closedMtx.RUnlock()
	return s.closed
}

func (s *serviceSet) setClosed() {
	s.closedMtx.Lock()
	defer s.closedMtx.Unlock()
	s.closed = true
}

type ConnEvent struct {
	Status ConnStatus
}

type ConnStatus uint8

const (
	ConnStatusConnected ConnStatus = iota
	ConnStatusDisconnected
	ConnStatusConnectFailed
)

// A Client maintains the RPC client connection and any active registered services, making sure
// they receive their heartbeat calls.
type Client struct {
	l                sync.Mutex
	client           *rpcplus.Client
	addr             string
	heartbeats       map[string]chan struct{}
	expandedAddrs    map[string]string
	names            map[string]string
	reconnecting     bool
	reconnMtx        sync.RWMutex
	clientMtx        sync.RWMutex
	closed           bool
	closedMtx        sync.RWMutex
	reconnectWatches map[chan ConnEvent]struct{}
}

func newClient(c *rpcplus.Client, addr string) *Client {
	return &Client{
		client:           c,
		addr:             addr,
		heartbeats:       make(map[string]chan struct{}),
		expandedAddrs:    make(map[string]string),
		names:            make(map[string]string),
		reconnectWatches: make(map[chan ConnEvent]struct{}),
	}
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
	return NewClientWithAddr(addr)
}

func NewClientWithAddr(addr string) (*Client, error) {
	c, err := rpcplus.DialHTTP("tcp", addr)
	return newClient(c, addr), err
}

func NewClientWithRPCClient(c *rpcplus.Client) *Client {
	return newClient(c, "")
}

var ErrDisconnected = errors.New("discover: no active rpc connection")

func (c *Client) newServiceSet(name string) (*serviceSet, error) {
	if c.isReconnecting() {
		return nil, ErrDisconnected
	}
	set := makeServiceSet(c)
	return set, <-set.bind(name)
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
	watch := set.Watch(true)
	defer set.Unwatch(watch)
	select {
	case <-watch:
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
	updates := set.Watch(true)
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
	err := c.call("Agent.Register", args, &ret, false)
	if err != nil {
		if err == ErrDisconnected {
			return err
		}
		return errors.New("discover: register failed: " + err.Error())
	}
	done := make(chan struct{})
	c.l.Lock()
	if ch, exists := c.heartbeats[args.Addr]; exists {
		// stop the old heartbeat if this is a re-registration
		close(ch)
	}
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
				// heartbeat
				err := c.call("Agent.Register", args, &ret, true)
				if err != nil {
					log.Printf("discover: heartbeat %s (%s) failed: %s", args.Name, args.Addr, err)
				}
			case <-done:
				return
			}
		}
	}()
	return nil
}

func (c *Client) rpcClient() *rpcplus.Client {
	c.clientMtx.RLock()
	defer c.clientMtx.RUnlock()
	return c.client
}

func (c *Client) call(method string, args interface{}, reply interface{}, waitForReconnect bool) error {
	if !waitForReconnect && c.isReconnecting() {
		return ErrDisconnected
	}
	client := c.rpcClient()
	err := client.Call(method, args, reply)
	if err == rpcplus.ErrShutdown || err == io.ErrUnexpectedEOF {
		if waitForReconnect {
			err = c.reconnect(client)
			if err != nil {
				return fmt.Errorf("%s, reconnect failed: %s", rpcplus.ErrShutdown, err)
			}
			// try again
			return c.call(method, args, reply, waitForReconnect)
		}
		go c.reconnect(client)
		return ErrDisconnected
	}
	return err
}

func (c *Client) streamGo(method string, args interface{}, reply interface{}) (*rpcplus.Client, *rpcplus.Call) {
	client := c.rpcClient()
	return client, client.StreamGo(method, args, reply)
}

func (c *Client) reconnect(disconnectedClient *rpcplus.Client) error {
	if c.addr == "" {
		c.notify(ConnStatusDisconnected)
		return errors.New("no reconnect address set")
	}
	c.clientMtx.Lock()
	defer c.clientMtx.Unlock()
	if disconnectedClient != c.client {
		// another goroutine has already reconnected
		return nil
	}
	c.setReconnecting(true)
	c.notify(ConnStatusDisconnected)
	for {
		if c.isClosed() {
			return ErrDisconnected
		}
		log.Printf("discover: reconnecting to %s", c.addr)
		client, err := rpcplus.DialHTTP("tcp", c.addr)
		if err != nil {
			c.notify(ConnStatusConnectFailed)
			log.Printf("discover: failed to reconnect to %s: %s", c.addr, err)
			time.Sleep(time.Second)
			continue
		}
		log.Printf("discover: reconnected to %s", c.addr)
		c.client = client
		c.setReconnecting(false)
		c.notify(ConnStatusConnected)
		return nil
	}
}

func (c *Client) isReconnecting() bool {
	c.reconnMtx.RLock()
	defer c.reconnMtx.RUnlock()
	return c.reconnecting
}

func (c *Client) setReconnecting(v bool) {
	c.reconnMtx.Lock()
	defer c.reconnMtx.Unlock()
	c.reconnecting = v
}

func (c *Client) WatchReconnects() chan ConnEvent {
	ch := make(chan ConnEvent)
	c.l.Lock()
	defer c.l.Unlock()
	c.reconnectWatches[ch] = struct{}{}
	return ch
}

func (c *Client) UnwatchReconnects(ch chan ConnEvent) {
	go func() {
		// drain channel to prevent deadlock
		for _ = range ch {
		}
	}()
	c.l.Lock()
	delete(c.reconnectWatches, ch)
	c.l.Unlock()
	close(ch)
}

func (c *Client) notify(s ConnStatus) {
	c.l.Lock()
	defer c.l.Unlock()
	for ch, _ := range c.reconnectWatches {
		ch <- ConnEvent{s}
	}
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
	err := c.call("Agent.Unregister", args, &struct{}{}, false)
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

func (c *Client) Close() error {
	c.l.Lock()
	defer c.l.Unlock()
	for _, ch := range c.heartbeats {
		close(ch)
	}
	c.setClosed()
	return c.rpcClient().Close()
}

func (c *Client) isClosed() bool {
	c.closedMtx.RLock()
	defer c.closedMtx.RUnlock()
	return c.closed
}

func (c *Client) setClosed() {
	c.closedMtx.Lock()
	defer c.closedMtx.Unlock()
	c.closed = true
}

// DefaultClient is used by all of the top-level functions. Connect must be
// called before using it directly.
var DefaultClient *Client
var defaultMtx sync.Mutex

// Connect explicitly initializes DefaultClient. If addr is empty, the default
// address is used. If DefaultClient has already been created it is a no-op.
func Connect(addr string) error {
	defaultMtx.Lock()
	defer defaultMtx.Unlock()
	return connectLocked(addr)
}

func connectLocked(addr string) (err error) {
	if DefaultClient != nil {
		return nil
	}
	if addr == "" {
		DefaultClient, err = NewClient()
		return
	}
	DefaultClient, err = NewClientWithAddr(addr)
	return
}

func ensureDefaultConnected() error {
	defaultMtx.Lock()
	defer defaultMtx.Unlock()
	return connectLocked("")
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
