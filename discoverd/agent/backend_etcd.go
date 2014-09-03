package agent

import (
	"encoding/json"
	"log"
	"strings"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
)

// KeyPrefix is used to create the full service path.
const KeyPrefix = "/discover"

// EtcdBackend for service discovery.
type EtcdBackend struct {
	Client *etcd.Client
}

func servicePath(name, addr string) string {
	if addr == "" {
		return KeyPrefix + "/services/" + name
	}
	return KeyPrefix + "/services/" + name + "/" + addr
}

// Subscribe to changes in services of a given name.
func (b *EtcdBackend) Subscribe(name string) (UpdateStream, error) {
	stream := &etcdStream{ch: make(chan *ServiceUpdate), stop: make(chan bool)}
	go func() {
		send := func(u *ServiceUpdate) bool {
			if u == nil {
				return true
			}
			select {
			case stream.ch <- u:
				return true
			case <-stream.stop:
				return false
			}
		}

		var sentinel bool
		keys := make(map[string]uint64)
		newKeys := make(map[string]uint64)
	sync:
		for {
			nextIndex := uint64(1)
			response, err := b.getCurrentState(name)
			if response != nil {
				for _, n := range response.Node.Nodes {
					if modified, ok := keys[n.Key]; ok && modified >= n.ModifiedIndex {
						newKeys[n.Key] = modified
						continue
					}
					if !send(b.responseToUpdate(response, n, newKeys)) {
						return
					}
				}
				nextIndex = response.EtcdIndex + 1
			} else if e, ok := err.(*etcd.EtcdError); ok {
				nextIndex = e.Index + 1
			}
			if !sentinel {
				if !send(&ServiceUpdate{}) {
					return
				}
				sentinel = true
			}
			for k := range keys {
				if _, ok := newKeys[k]; ok {
					continue
				}
				serviceName, serviceAddr := splitServiceNameAddr(k)
				if serviceName == "" {
					continue
				}
				// instance was deleted, send offline update
				if !send(&ServiceUpdate{Name: serviceName, Addr: serviceAddr}) {
					return
				}
			}
			keys = newKeys
			newKeys = make(map[string]uint64)

			path := servicePath(name, "")
			for {
				watch := make(chan *etcd.Response)
				watchDone := make(chan struct{})
				var watchErr error
				go func() {
					_, watchErr = b.Client.Watch(path, nextIndex, true, watch, stream.stop)
					close(watchDone)
				}()
				for resp := range watch {
					if !send(b.responseToUpdate(resp, resp.Node, keys)) {
						return
					}
					nextIndex = resp.EtcdIndex + 1
				}
				<-watchDone
				select {
				case <-stream.stop:
					return
				default:
				}
				if e, ok := watchErr.(*etcd.EtcdError); ok && e.ErrorCode == 401 {
					// event log has been pruned beyond our waitIndex, force full sync
					log.Printf("Got etcd error 401, doing full sync")
					continue sync
				}
				log.Printf("Restarting etcd watch %s due to error: %s", path, watchErr)
			}
		}
	}()
	return stream, nil
}

type etcdStream struct {
	ch       chan *ServiceUpdate
	stop     chan bool
	stopOnce sync.Once
}

func (s *etcdStream) Chan() chan *ServiceUpdate { return s.ch }

func (s *etcdStream) Close() { s.stopOnce.Do(func() { close(s.stop) }) }

func (b *EtcdBackend) responseToUpdate(resp *etcd.Response, node *etcd.Node, keys map[string]uint64) *ServiceUpdate {
	keys[node.Key] = node.ModifiedIndex
	serviceName, serviceAddr := splitServiceNameAddr(node.Key)
	if serviceName == "" {
		return nil
	}
	if "get" == resp.Action || ("set" == resp.Action || "update" == resp.Action) && (resp.PrevNode == nil || node.Value != resp.PrevNode.Value) {
		// GET is because getCurrentState returns responses of Action GET.
		// some SETs are heartbeats, so we ignore SETs where value didn't change.
		var serviceAttrs map[string]string
		err := json.Unmarshal([]byte(node.Value), &serviceAttrs)
		if err != nil {
			return nil
		}
		return &ServiceUpdate{
			Name:    serviceName,
			Addr:    serviceAddr,
			Online:  true,
			Attrs:   serviceAttrs,
			Created: uint(node.CreatedIndex),
		}
	} else if "delete" == resp.Action || "expire" == resp.Action {
		delete(keys, node.Key)
		return &ServiceUpdate{
			Name: serviceName,
			Addr: serviceAddr,
		}
	} else {
		return nil
	}
}

func splitServiceNameAddr(key string) (string, string) {
	// expected key structure: /PREFIX/services/NAME/ADDR
	splitKey := strings.SplitN(key, "/", 5)
	if len(splitKey) < 5 {
		return "", ""
	}
	return splitKey[3], splitKey[4]
}

func (b *EtcdBackend) getCurrentState(name string) (*etcd.Response, error) {
	return b.Client.Get(servicePath(name, ""), false, true)
}

// Register a service with etcd.
func (b *EtcdBackend) Register(name, addr string, attrs map[string]string) error {
	attrsJSON, err := json.Marshal(attrs)
	if err != nil {
		return err
	}
	attrsString := string(attrsJSON)
	path := servicePath(name, addr)
	ttl := uint64(HeartbeatIntervalSecs + MissedHearbeatTTL)

	_, err = b.Client.Update(path, attrsString, ttl)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
		// This is a workaround for etcd issue #407: https://github.com/coreos/etcd/issues/407
		// If we just do a Set and don't try to Update first, createdIndex will get incremented
		// on each heartbeat, breaking leader election.
		_, err = b.Client.Set(path, attrsString, ttl)
	}
	return err
}

// Unregister a service with etcd.
func (b *EtcdBackend) Unregister(name, addr string) error {
	_, err := b.Client.Delete(servicePath(name, addr), false)
	return err
}
