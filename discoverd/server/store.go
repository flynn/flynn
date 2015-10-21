package server

import (
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/hashicorp/raft"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/hashicorp/raft-boltdb"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/discoverd/client"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/stream"
)

const (
	// DefaultInstanceTTL is the length of time after a heartbeat from an instance before it expires.
	DefaultInstanceTTL = 10 * time.Second

	// DefaultExpiryCheckInterval is the default interval between checks for expired instances.
	DefaultExpiryCheckInterval = 1 * time.Second
)

var logger = log15.New("component", "discoverd")

// DefaultServiceConfig is the default configuration for a service when one is not specified.
var DefaultServiceConfig = &discoverd.ServiceConfig{
	LeaderType: discoverd.LeaderTypeOldest,
}

var (
	ErrUnsetService = errors.New("discoverd: service name must not be empty")

	ErrInvalidService = errors.New("discoverd: service must be lowercase alphanumeric plus dash")

	ErrSendBlocked = errors.New("discoverd: channel send failed due to blocked receiver")

	ErrBindAddressRequired = errors.New("discoverd: bind address required")

	ErrAdvertiseRequired = errors.New("discoverd: advertised address required")

	// ErrNotLeader is returned when performing an operation on the store when
	// it is not the current cluster leader.
	ErrNotLeader = errors.New("discoverd: not leader")

	// ErrNoKnownLeader is returned when there is not a current know cluster leader.
	ErrNoKnownLeader = errors.New("discoverd: no known leader")
)

// Store represents a storage backend using the raft protocol.
type Store struct {
	mu          sync.RWMutex
	path        string // root store path
	logger      *log.Logger
	raft        *raft.Raft
	transport   *raft.NetworkTransport
	peerStore   raft.PeerStore
	stableStore *raftboltdb.BoltStore

	data        *raftData
	subscribers map[string]*list.List

	// Goroutine management
	wg      sync.WaitGroup
	closing chan struct{}

	// The address the raft TCP port binds to.
	BindAddress string

	// The address the raft server uses to represent itself in the peer list.
	Advertise net.Addr

	// Raft settings.
	HeartbeatTimeout   time.Duration
	ElectionTimeout    time.Duration
	LeaderLeaseTimeout time.Duration
	CommitTimeout      time.Duration
	EnableSingleNode   bool

	// The writer where logs are written to.
	LogOutput io.Writer

	// The duration without a heartbeat before an instance is expired.
	InstanceTTL time.Duration

	// The interval between checks for instance expiry on the leader.
	ExpiryCheckInterval time.Duration

	// Returns the current time.
	// This defaults to time.Now and can be changed for mocking.
	Now func() time.Time
}

// NewStore returns an instance of Store.
func NewStore(path string) *Store {
	return &Store{
		path:        path,
		data:        newRaftData(),
		subscribers: make(map[string]*list.List),

		closing: make(chan struct{}),

		HeartbeatTimeout:   1000 * time.Millisecond,
		ElectionTimeout:    1000 * time.Millisecond,
		LeaderLeaseTimeout: 500 * time.Millisecond,
		CommitTimeout:      50 * time.Millisecond,

		InstanceTTL:         DefaultInstanceTTL,
		ExpiryCheckInterval: DefaultExpiryCheckInterval,

		LogOutput: os.Stderr,
		Now:       time.Now,
	}
}

// Path returns the path that the store was initialized with.
func (s *Store) Path() string { return s.path }

// Open starts the raft consensus and opens the store.
func (s *Store) Open() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Set up logging.
	s.logger = log.New(s.LogOutput, "[discoverd] ", log.LstdFlags)

	// Require bind address & advertise address.
	if s.BindAddress == "" {
		return ErrBindAddressRequired
	} else if s.Advertise == nil {
		return ErrAdvertiseRequired
	}

	// Create root directory.
	if err := os.MkdirAll(s.path, 0777); err != nil {
		return err
	}

	// Create raft configuration.
	config := raft.DefaultConfig()
	config.HeartbeatTimeout = s.HeartbeatTimeout
	config.ElectionTimeout = s.ElectionTimeout
	config.LeaderLeaseTimeout = s.LeaderLeaseTimeout
	config.CommitTimeout = s.CommitTimeout
	config.LogOutput = s.LogOutput
	config.EnableSingleNode = s.EnableSingleNode

	// Begin listening to TCP port.
	trans, err := raft.NewTCPTransport(s.BindAddress, s.Advertise, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return fmt.Errorf("opening tcp transport: %s", err)
	}
	s.transport = trans

	// Setup storage layers.
	s.peerStore = raft.NewJSONPeers(s.path, s.transport)
	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(s.path, "raft.db"))
	if err != nil {
		return fmt.Errorf("stable store: %s", err)
	}
	s.stableStore = stableStore

	// Create the snapshot store.
	ss, err := raft.NewFileSnapshotStore(s.path, 2, os.Stderr)
	if err != nil {
		return fmt.Errorf("snapshot store: %s", err)
	}

	// Create raft log.
	r, err := raft.NewRaft(config, s, stableStore, stableStore, ss, s.peerStore, s.transport)
	if err != nil {
		return fmt.Errorf("raft: %s", err)
	}
	s.raft = r

	// Start goroutine to check for instance expiry.
	s.wg.Add(1)
	go s.expirer()

	return nil
}

// Close shuts down the transport and store.
func (s *Store) Close() error {
	// Notify goroutines of closing and wait until they finish.
	close(s.closing)
	s.wg.Wait()

	if s.raft != nil {
		s.raft.Shutdown()
		s.raft = nil
	}
	if s.transport != nil {
		s.transport.Close()
		s.transport = nil
	}
	if s.stableStore != nil {
		s.stableStore.Close()
		s.stableStore = nil
	}

	return nil
}

// Leader returns the host of the current leader. Returns empty string if there is no leader.
// Panic if called before store is opened.
func (s *Store) Leader() string {
	if s.raft == nil {
		return ""
	}
	return s.raft.Leader()
}

// LeaderCh returns a channel that signals leadership change.
// Panic if called before store is opened.
func (s *Store) LeaderCh() <-chan bool {
	if s.raft == nil {
		ch := make(chan bool, 1)
		ch <- true
		return ch
	}
	return s.raft.LeaderCh()
}

// AddPeer adds a peer to the raft cluster. Panic if store is not open yet.
func (s *Store) AddPeer(peer string) error {
	return s.raft.AddPeer(peer).Error()
}

// RemovePeer removes a peer from the raft cluster. Panic if store is not open yet.
func (s *Store) RemovePeer(peer string) error {
	return s.raft.RemovePeer(peer).Error()
}

// SetPeers sets a list of peers in the raft cluster. Panic if store is not open yet.
func (s *Store) SetPeers(peers []string) error {
	return s.raft.SetPeers(peers).Error()
}

// ServiceNames returns a sorted list of existing service names.
func (s *Store) ServiceNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var a []string
	for name := range s.data.Services {
		a = append(a, name)
	}
	sort.Strings(a)

	return a
}

// AddService creates a service with a configuration.
// Returns an error if the service already exists.
func (s *Store) AddService(service string, config *discoverd.ServiceConfig) error {
	if config == nil {
		config = DefaultServiceConfig
	}

	// Serialize command.
	cmd, err := json.Marshal(&addServiceCommand{
		Service: service,
		Config:  config,
	})
	if err != nil {
		return err
	}

	if _, err := s.raftApply(addServiceCommandType, cmd); err != nil {
		return err
	}
	return err
}

func (s *Store) applyAddServiceCommand(cmd []byte) error {
	var c addServiceCommand
	if err := json.Unmarshal(cmd, &c); err != nil {
		return err
	}

	// Verify that the service doesn't already exist.
	if s.data.Services[c.Service] != nil {
		return ServiceExistsError(c.Service)
	}

	// Create new named service with configuration.
	s.data.Services[c.Service] = c.Config

	return nil
}

// Config returns the configuration for service.
func (s *Store) Config(service string) *discoverd.ServiceConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Services[service]
}

// RemoveService deletes the service from the store.
func (s *Store) RemoveService(service string) error {
	// Serialize command.
	cmd, err := json.Marshal(&removeServiceCommand{Service: service})
	if err != nil {
		return err
	}

	if _, err := s.raftApply(removeServiceCommandType, cmd); err != nil {
		return err
	}
	return nil
}

func (s *Store) applyRemoveServiceCommand(cmd []byte) error {
	var c removeServiceCommand
	if err := json.Unmarshal(cmd, &c); err != nil {
		return err
	}

	// Verify that the service exists.
	if s.data.Services[c.Service] == nil {
		return NotFoundError{Service: c.Service}
	}

	// Remove the service.
	delete(s.data.Services, c.Service)

	// Broadcast EventKindDown for all instances on the service.
	for _, inst := range s.data.ServiceInstances(c.Service) {
		s.broadcast(&discoverd.Event{
			Service:  c.Service,
			Kind:     discoverd.EventKindDown,
			Instance: inst.Instance,
		})
	}

	return nil
}

// Instances returns a list of instances for service.
func (s *Store) Instances(service string) ([]*discoverd.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.instances(service), nil
}

func (s *Store) instances(service string) []*discoverd.Instance {
	var a []*discoverd.Instance
	for _, inst := range s.data.Instances[service] {
		var other = *inst.Instance
		a = append(a, &other)
	}
	sort.Sort(instanceSlice(a))
	return a
}

func (s *Store) AddInstance(service string, inst *discoverd.Instance) error {
	now := s.Now()

	// Serialize command.
	cmd, err := json.Marshal(&addInstanceCommand{
		Service:    service,
		Instance:   inst,
		ExpiryTime: now.Add(s.InstanceTTL),
		Now:        now,
	})
	if err != nil {
		return err
	}

	if _, err := s.raftApply(addInstanceCommandType, cmd); err != nil {
		return err
	}
	return nil
}

func (s *Store) applyAddInstanceCommand(cmd []byte, index uint64) error {
	var c addInstanceCommand
	if err := json.Unmarshal(cmd, &c); err != nil {
		return err
	}

	// Verify that the service exists.
	if s.data.Services[c.Service] == nil {
		return NotFoundError{Service: c.Service}
	}

	// Save the instance data.
	if s.data.Instances[c.Service] == nil {
		s.data.Instances[c.Service] = make(map[string]instanceEntry)
	}

	// Check if the instance already exists.
	// If it does then copy the original index.
	// Otherwise set the index to the current log entry's index.
	prev, existing := s.data.Instances[c.Service][c.Instance.ID]
	if existing {
		c.Instance.Index = prev.Instance.Index
	} else {
		c.Instance.Index = index
	}

	// Check if the existing instance is being updated.
	updating := existing && !c.Instance.Equal(s.data.Instances[c.Service][c.Instance.ID].Instance)

	// Update entry.
	s.data.Instances[c.Service][c.Instance.ID] = instanceEntry{
		Instance:   c.Instance,
		ExpiryTime: c.ExpiryTime,
	}

	// Broadcast "up" event if new instance.
	if !existing {
		s.broadcast(&discoverd.Event{
			Service:  c.Service,
			Kind:     discoverd.EventKindUp,
			Instance: c.Instance,
		})
	} else if updating {
		s.broadcast(&discoverd.Event{
			Service:  c.Service,
			Kind:     discoverd.EventKindUpdate,
			Instance: c.Instance,
		})
	}

	// Update service leader, if necessary.
	s.invalidateServiceLeader(c.Service, c.Now)

	return nil
}

func (s *Store) RemoveInstance(service, id string) error {
	// Serialize command.
	cmd, err := json.Marshal(&removeInstanceCommand{
		Service: service,
		ID:      id,
		Now:     s.Now(),
	})
	if err != nil {
		return err
	}

	if _, err := s.raftApply(removeInstanceCommandType, cmd); err != nil {
		return err
	}
	return nil
}

func (s *Store) applyRemoveInstanceCommand(cmd []byte) error {
	var c removeInstanceCommand
	if err := json.Unmarshal(cmd, &c); err != nil {
		return err
	}

	// Verify that the service exists.
	if s.data.Instances[c.Service] == nil {
		return NotFoundError{Service: c.Service}
	}

	// Remove instance data.
	entry := s.data.Instances[c.Service][c.ID]
	delete(s.data.Instances[c.Service], c.ID)

	// Broadcast "down" event for instance.
	if entry.Instance != nil {
		s.broadcast(&discoverd.Event{
			Service:  c.Service,
			Kind:     discoverd.EventKindDown,
			Instance: entry.Instance,
		})
	}

	// Invalidate service leadership.
	s.invalidateServiceLeader(c.Service, c.Now)

	return nil
}

// ServiceMeta returns the meta data for a service.
func (s *Store) ServiceMeta(service string) *discoverd.ServiceMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serviceMeta(service)
}

func (s *Store) serviceMeta(service string) *discoverd.ServiceMeta {
	return s.data.Metas[service]
}

func (s *Store) SetServiceMeta(service string, meta *discoverd.ServiceMeta) error {
	// Serialize command.
	cmd, err := json.Marshal(&setServiceMetaCommand{
		Service: service,
		Meta:    meta,
	})
	if err != nil {
		return err
	}

	index, err := s.raftApply(setServiceMetaCommandType, cmd)
	if err != nil {
		return err
	}
	meta.Index = index

	return nil
}

func (s *Store) applySetServiceMetaCommand(cmd []byte, index uint64) error {
	var c setServiceMetaCommand
	if err := json.Unmarshal(cmd, &c); err != nil {
		return err
	}

	// Verify that the service exists.
	service := s.data.Services[c.Service]
	if service == nil {
		return NotFoundError{Service: c.Service}
	}

	// If no index is provided then the meta should not be set.
	curr := s.data.Metas[c.Service]
	if c.Meta.Index == 0 {
		if curr != nil {
			return hh.ObjectExistsErr(fmt.Sprintf("Service metadata for %q already exists, use index=n to set", c.Service))
		}
	} else {
		if curr == nil {
			return hh.PreconditionFailedErr(fmt.Sprintf("Service metadata for %q does not exist, use index=0 to set", c.Service))
		} else if curr.Index != c.Meta.Index {
			return hh.PreconditionFailedErr(fmt.Sprintf("Service metadata for %q exists, but wrong index provided", c.Service))
		}
	}

	// Update the meta and set the index.
	c.Meta.Index = index
	s.data.Metas[c.Service] = c.Meta

	// Broadcast EventKindServiceMeta event.
	s.broadcast(&discoverd.Event{
		Service:     c.Service,
		Kind:        discoverd.EventKindServiceMeta,
		ServiceMeta: c.Meta,
	})

	return nil
}

// SetServiceLeader manually sets the leader for a service.
func (s *Store) SetServiceLeader(service, id string) error {
	// Serialize command.
	cmd, err := json.Marshal(&setLeaderCommand{
		Service: service,
		ID:      id,
	})
	if err != nil {
		return err
	}

	if _, err := s.raftApply(setLeaderCommandType, cmd); err != nil {
		return err
	}
	return nil
}

func (s *Store) applySetLeaderCommand(cmd []byte) error {
	var c setLeaderCommand
	if err := json.Unmarshal(cmd, &c); err != nil {
		return err
	}

	s.data.Leaders[c.Service] = c.ID

	// Notify new leadership.
	if entry, ok := s.data.Instances[c.Service][c.ID]; ok && entry.Instance != nil {
		s.broadcast(&discoverd.Event{
			Service:  c.Service,
			Kind:     discoverd.EventKindLeader,
			Instance: entry.Instance,
		})
	}

	return nil
}

func (s *Store) ServiceLeader(service string) (*discoverd.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serviceLeader(service), nil
}

func (s *Store) serviceLeader(service string) *discoverd.Instance {
	// Find instance ID of the leader.
	instanceID := s.data.Leaders[service]

	// Ignore if there are no instances on the service.
	m := s.data.Instances[service]
	if m == nil {
		return nil
	}
	if _, ok := m[instanceID]; !ok {
		return nil
	}

	// Return instance specified by the leader id.
	return m[instanceID].Instance
}

// invalidateServiceLeader updates the current leader of service.
func (s *Store) invalidateServiceLeader(service string, now time.Time) {
	// Retrieve service config.
	c := s.data.Services[service]

	// Ignore if there is no config or the leader is manually elected.
	if c == nil || c.LeaderType == discoverd.LeaderTypeManual {
		return
	}

	// Retrieve current leader ID.
	prevLeaderID := s.data.Leaders[service]

	// Find the oldest, non-expired instance.
	var leader *discoverd.Instance
	for _, entry := range s.data.Instances[service] {
		// Ignore expired entries.
		if entry.ExpiryTime.Before(now) {
			continue
		}

		// Set leader if there is no leader or if the index is older.
		if leader == nil || entry.Instance.Index < leader.Index {
			leader = entry.Instance
		}
	}

	// Retrieve the leader ID.
	var leaderID string
	if leader != nil {
		leaderID = leader.ID
	}

	// Set leader.
	s.data.Leaders[service] = leaderID

	// Broadcast event.
	if prevLeaderID != leaderID {
		var inst *discoverd.Instance
		if s.data.Instances[service] != nil && s.data.Instances[service][leaderID].Instance != nil {
			inst = s.data.Instances[service][leaderID].Instance
		}

		s.broadcast(&discoverd.Event{
			Service:  service,
			Kind:     discoverd.EventKindLeader,
			Instance: inst,
		})
	}
}

// expirer runs in a separate goroutine and checks for instance expiration.
func (s *Store) expirer() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.ExpiryCheckInterval)
	defer ticker.Stop()

	for {
		// Wait for next check or for close signal.
		select {
		case <-s.closing:
			return
		case <-ticker.C:
		}

		// Check all instances for expiration.
		if err := s.EnforceExpiry(); err != nil {
			s.logger.Printf("enforce expiry: %s", err)
		}
	}
}

// EnforceExpiry checks all instances for expiration and issues an expiration command, if necessary.
// This function returns raft.ErrNotLeader if this store is not the current leader.
func (s *Store) EnforceExpiry() error {
	var cmd []byte
	if err := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		// Ignore if this store is not the leader.
		if s.raft.Leader() != s.Advertise.String() {
			return raft.ErrNotLeader
		}

		now := time.Now()

		// Iterate over services and then instances.
		var instances []expireInstance
		for service, m := range s.data.Instances {
			for _, inst := range m {
				// Ignore instances expiring in the future.
				if inst.ExpiryTime.After(now) {
					continue
				}

				// Add to list of instances to expire.
				// The current expiry time is added to prevent a race condition of
				// instances updating their expiry date while this command is applying.
				instances = append(instances, expireInstance{
					Service:    service,
					InstanceID: inst.Instance.ID,
					ExpiryTime: inst.ExpiryTime,
				})
			}
		}

		// If we have no instances to expire then exit.
		if len(instances) == 0 {
			return nil
		}

		// Create command to expire instances.
		buf, err := json.Marshal(&expireInstancesCommand{
			Instances: instances,
			Now:       now,
		})
		if err != nil {
			return err
		}
		cmd = buf

		return nil
	}(); err != nil {
		return err
	} else if cmd == nil {
		return nil
	}

	// Apply command to raft.
	if _, err := s.raftApply(expireInstancesCommandType, cmd); err != nil {
		return err
	}
	return nil
}

func (s *Store) applyExpireInstancesCommand(cmd []byte) error {
	var c expireInstancesCommand
	if err := json.Unmarshal(cmd, &c); err != nil {
		return err
	}

	// Iterate over instances and remove ones with matching expiry times.
	services := make(map[string]struct{})
	for _, inst := range c.Instances {
		// Ignore if the service no longers exists.
		m := s.data.Instances[inst.Service]
		if m == nil {
			continue
		}

		// Ignore if entry doesn't exist or expiry time is different.
		entry, ok := m[inst.InstanceID]
		if !ok || !entry.ExpiryTime.Equal(inst.ExpiryTime) {
			continue
		}

		// Remove instance.
		delete(m, inst.InstanceID)

		// Broadcast down event.
		s.broadcast(&discoverd.Event{
			Service:  inst.Service,
			Kind:     discoverd.EventKindDown,
			Instance: entry.Instance,
		})

		// Keep track of services invalidated.
		services[inst.Service] = struct{}{}
	}

	// Invalidate all services that had expirations.
	for service := range services {
		s.invalidateServiceLeader(service, c.Now)
	}

	return nil
}

// raftApply joins typ and cmd and applies it to raft.
// This call blocks until the apply completes and returns the error.
func (s *Store) raftApply(typ byte, cmd []byte) (uint64, error) {
	// Join the command type and data into one message.
	buf := append([]byte{typ}, cmd...)

	// Apply to raft and receive an ApplyFuture back.
	f := s.raft.Apply(buf, 5*time.Second)
	if err := f.Error(); err == raft.ErrNotLeader {
		return 0, ErrNotLeader // hide underlying implementation error
	} else if err != nil {
		return f.Index(), err
	} else if err, ok := f.Response().(error); ok {
		return f.Index(), err
	}

	return f.Index(), nil
}

func (s *Store) Apply(l *raft.Log) interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Require at least a "command type" header byte.
	if len(l.Data) == 0 {
		return errors.New("no log data found")
	}

	// Extract the command type and data.
	typ, cmd := l.Data[0], l.Data[1:]

	// Determine the command type by the first byte.
	switch typ {
	case addServiceCommandType:
		return s.applyAddServiceCommand(cmd)
	case removeServiceCommandType:
		return s.applyRemoveServiceCommand(cmd)
	case setServiceMetaCommandType:
		return s.applySetServiceMetaCommand(cmd, l.Index)
	case setLeaderCommandType:
		return s.applySetLeaderCommand(cmd)
	case addInstanceCommandType:
		return s.applyAddInstanceCommand(cmd, l.Index)
	case removeInstanceCommandType:
		return s.applyRemoveInstanceCommand(cmd)
	case expireInstancesCommandType:
		return s.applyExpireInstancesCommand(cmd)
	default:
		return fmt.Errorf("invalid command type: %d", typ)
	}
}

// Snapshot implements raft.FSM.
func (s *Store) Snapshot() (raft.FSMSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	buf, err := json.Marshal(s.data)
	if err != nil {
		return nil, err
	}
	return &raftSnapshot{data: buf}, nil
}

// Restore implements raft.FSM.
func (s *Store) Restore(r io.ReadCloser) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := &raftData{}
	if err := json.NewDecoder(r).Decode(data); err != nil {
		return err
	}
	s.data = data
	return nil
}

// Subscribe creates a subscription to events on a given service.
func (s *Store) Subscribe(service string, sendCurrent bool, kinds discoverd.EventKind, ch chan *discoverd.Event) stream.Stream {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create service subscription list if it doesn't exist yet.
	if _, ok := s.subscribers[service]; !ok {
		s.subscribers[service] = list.New()
	}

	// Create and add subscription.
	sub := &subscription{
		kinds:   kinds,
		ch:      ch,
		store:   s,
		service: service,
	}
	sub.el = s.subscribers[service].PushBack(sub)

	// Send current instances.
	if sendCurrent && kinds.Any(discoverd.EventKindUp) {
		for _, inst := range s.instances(service) {
			ch <- &discoverd.Event{
				Service:  service,
				Kind:     discoverd.EventKindUp,
				Instance: inst,
			}
			// TODO: add a timeout to sends so that clients can't slow things down too much
		}
	}

	// Send current leader.
	if leader := s.serviceLeader(service); sendCurrent && kinds&discoverd.EventKindLeader != 0 && leader != nil {
		ch <- &discoverd.Event{
			Service:  service,
			Kind:     discoverd.EventKindLeader,
			Instance: leader,
		}
	}

	// Send current service meta data.
	if meta := s.serviceMeta(service); sendCurrent && kinds.Any(discoverd.EventKindServiceMeta) && meta != nil {
		ch <- &discoverd.Event{
			Service:     service,
			Kind:        discoverd.EventKindServiceMeta,
			ServiceMeta: meta,
		}
	}

	// Send current service.
	if sendCurrent && kinds.Any(discoverd.EventKindCurrent) {
		ch <- &discoverd.Event{
			Service: service,
			Kind:    discoverd.EventKindCurrent,
		}
	}

	return sub
}

// broadcast sends an event to all subscribers.
// Requires the mu lock to be obtained.
func (s *Store) broadcast(event *discoverd.Event) {
	logBroadcast(event)

	// Retrieve list of subscribers for the service.
	l, ok := s.subscribers[event.Service]

	if !ok {
		return
	}

	// Iterate over each subscriber in the list.
	for el := l.Front(); el != nil; el = el.Next() {
		sub := el.Value.(*subscription)

		// Skip if event type is not subscribed to.
		if sub.kinds&event.Kind == 0 {
			continue
		}

		// Send event to subscriber.
		// If subscriber is blocked then close it.
		select {
		case sub.ch <- event:
		default:
			sub.err = ErrSendBlocked
			go sub.Close()
		}
	}
}

func logBroadcast(event *discoverd.Event) {
	log := logger.New("fn", "broadcast")
	ctx := []interface{}{
		"event", event.Kind,
		"service", event.Service,
	}
	if event.Instance != nil {
		ctx = append(ctx, []interface{}{
			"instance.id", event.Instance.ID,
			"instance.addr", event.Instance.Addr,
		}...)
	}
	if event.ServiceMeta != nil {
		ctx = append(ctx, []interface{}{"service_meta.index", event.ServiceMeta.Index, "service_meta.data", string(event.ServiceMeta.Data)}...)
	}
	log.Info(fmt.Sprintf("broadcasting %s event", event.Kind), ctx...)
}

// raftSnapshot implements raft.FSMSnapshot.
// The FSM is serialized on snapshot creation so this simply writes to the sink.
type raftSnapshot struct {
	data []byte
}

// Persist writes the snapshot to the sink.
func (ss *raftSnapshot) Persist(sink raft.SnapshotSink) error {
	// Write data to sink.
	if _, err := sink.Write(ss.data); err != nil {
		sink.Cancel()
		return err
	}

	// Close and exit.
	return sink.Close()
}

// Release implements raft.FSMSnapshot. This is a no-op.
func (ss *raftSnapshot) Release() {}

// instanceEntry represents an instance with a TTL.
type instanceEntry struct {
	Instance   *discoverd.Instance
	ExpiryTime time.Time
}

// instanceEntries represents a sortable list of entries by index.
type instanceEntries []instanceEntry

func (a instanceEntries) Len() int           { return len(a) }
func (a instanceEntries) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a instanceEntries) Less(i, j int) bool { return a[i].Instance.Index < a[j].Instance.Index }

// instanceSlice represents a sortable list of instances by id.
type instanceSlice []*discoverd.Instance

func (a instanceSlice) Len() int           { return len(a) }
func (a instanceSlice) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a instanceSlice) Less(i, j int) bool { return a[i].ID < a[j].ID }

// Command type header bytes.
const (
	addServiceCommandType      = byte(0)
	removeServiceCommandType   = byte(1)
	setServiceMetaCommandType  = byte(2)
	setLeaderCommandType       = byte(3)
	addInstanceCommandType     = byte(4)
	removeInstanceCommandType  = byte(5)
	expireInstancesCommandType = byte(6)
)

// addServiceCommand represents a command object to create a service.
type addServiceCommand struct {
	Service string
	Config  *discoverd.ServiceConfig
}

// removeServiceCommand represents a command object to delete a service.
type removeServiceCommand struct {
	Service string
}

// setServiceMetaCommand represents a command object to set meta on a service.
type setServiceMetaCommand struct {
	Service string
	Meta    *discoverd.ServiceMeta
}

// setLeaderCommand represents a command object to manually assign a leader to a service.
type setLeaderCommand struct {
	Service string
	ID      string
}

// addInstanceCommand represents a command object to add an instance.
type addInstanceCommand struct {
	Service    string
	Instance   *discoverd.Instance
	ExpiryTime time.Time
	Now        time.Time // deterministic time for leader invalidation
}

// removeInstanceCommand represents a command object to remove an instance.
type removeInstanceCommand struct {
	Service string
	ID      string
	Now     time.Time // deterministic time for leader invalidation
}

// expireInstancesCommand represents a command object to expire multiple instances.
type expireInstancesCommand struct {
	Instances []expireInstance
	Now       time.Time // deterministic time for leader invalidation
}

// expireInstance represents a single instance to expire.
type expireInstance struct {
	Service    string
	InstanceID string
	ExpiryTime time.Time // must match during apply
}

// raftData represents the root data structure for the raft store.
type raftData struct {
	Services  map[string]*discoverd.ServiceConfig `json:"services,omitempty"`
	Metas     map[string]*discoverd.ServiceMeta   `json:"metas,omitempty"`
	Leaders   map[string]string                   `json:"leaders,omitempty"`
	Instances map[string]map[string]instanceEntry `json:"instances,omitempty"`
}

func newRaftData() *raftData {
	return &raftData{
		Services:  make(map[string]*discoverd.ServiceConfig),
		Metas:     make(map[string]*discoverd.ServiceMeta),
		Leaders:   make(map[string]string),
		Instances: make(map[string]map[string]instanceEntry),
	}
}

// ServiceInstances returns the instances of a service in sorted order.
func (d *raftData) ServiceInstances(service string) []instanceEntry {
	a := make([]instanceEntry, 0, len(d.Instances[service]))
	for _, entry := range d.Instances[service] {
		a = append(a, entry)
	}

	sort.Sort(instanceEntries(a))
	return a
}

// subscription represents a listener to one or more kinds of events.
type subscription struct {
	kinds discoverd.EventKind
	ch    chan *discoverd.Event
	err   error

	// the following fields are used by Close to clean up
	el      *list.Element
	store   *Store
	service string
	closed  bool
}

func (s *subscription) Err() error { return s.err }

func (s *subscription) Close() error {
	go func() {
		// drain channel to prevent deadlocks
		for range s.ch {
		}
	}()

	s.close()
	return nil
}

func (s *subscription) close() {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	if s.closed {
		return
	}

	l := s.store.subscribers[s.service]
	l.Remove(s.el)
	if l.Len() == 0 {
		delete(s.store.subscribers, s.service)
	}
	close(s.ch)

	s.closed = true
}

type NotFoundError struct {
	Service  string
	Instance string
}

func (e NotFoundError) Error() string {
	if e.Instance == "" {
		return fmt.Sprintf("discoverd: service %q not found", e.Service)
	}
	return fmt.Sprintf("discoverd: instance %s/%s not found", e.Service, e.Instance)
}

func IsNotFound(err error) bool {
	_, ok := err.(NotFoundError)
	return ok
}

type ServiceExistsError string

func (e ServiceExistsError) Error() string {
	return fmt.Sprintf("discoverd: service %q already exists", string(e))
}

func IsServiceExists(err error) bool {
	_, ok := err.(ServiceExistsError)
	return ok
}

// ValidServiceName returns nil if service is valid. Otherwise returns an error.
func ValidServiceName(service string) error {
	// Blank service names are not allowed.
	if service == "" {
		return ErrUnsetService
	}

	// Service names must consist of the characters [a-z0-9-]
	for _, r := range service {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return ErrInvalidService
		}
	}

	return nil
}

// ProxyStore implements some of the Store methods as proxy calls.
// Only the subset of methods required for DNSServer.Store are implemented.
type ProxyStore struct {
	Peers []string
}

// Instances returns a list of instances for a service.
func (s *ProxyStore) Instances(service string) ([]*discoverd.Instance, error) {
	host := s.Peers[rand.Intn(len(s.Peers))]
	client := discoverd.NewClientWithURL("http://" + host)
	return client.Service(service).Instances()
}

// ServiceLeader returns the leader for a service.
func (s *ProxyStore) ServiceLeader(service string) (*discoverd.Instance, error) {
	host := s.Peers[rand.Intn(len(s.Peers))]
	client := discoverd.NewClientWithURL("http://" + host)
	return client.Service(service).Leader()
}
