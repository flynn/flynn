package agent

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/coreos/go-etcd/etcd"
)

const KeyPrefix = "/discover"

type EtcdBackend struct {
	Client *etcd.Client
}

func servicePath(name, addr string) string {
	if addr == "" {
		return KeyPrefix + "/services/" + name
	}
	return KeyPrefix + "/services/" + name + "/" + addr
}

func (b *EtcdBackend) Subscribe(name string) (UpdateStream, error) {
	stream := &etcdStream{ch: make(chan *ServiceUpdate), stop: make(chan bool)}
	watch := b.getStateChanges(name, stream.stop)
	response, _ := b.getCurrentState(name)
	go func() {
		if response != nil {
			for _, n := range response.Node.Nodes {
				if update := b.responseToUpdate(response, &n); update != nil {
					stream.ch <- update
				}
			}
		}
		stream.ch <- &ServiceUpdate{}
		for resp := range watch {
			if update := b.responseToUpdate(resp, resp.Node); update != nil {
				stream.ch <- update
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

func (b *EtcdBackend) responseToUpdate(resp *etcd.Response, node *etcd.Node) *ServiceUpdate {
	// expected key structure: /PREFIX/services/NAME/ADDR
	splitKey := strings.SplitN(node.Key, "/", 5)
	if len(splitKey) < 5 {
		return nil
	}
	serviceName := splitKey[3]
	serviceAddr := splitKey[4]
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
		return &ServiceUpdate{
			Name: serviceName,
			Addr: serviceAddr,
		}
	} else {
		return nil
	}
}

func (b *EtcdBackend) getCurrentState(name string) (*etcd.Response, error) {
	return b.Client.Get(servicePath(name, ""), false, true)
}

func (b *EtcdBackend) getStateChanges(name string, stop chan bool) chan *etcd.Response {
	watch := make(chan *etcd.Response)
	go b.Client.Watch(servicePath(name, ""), 0, true, watch, stop)
	return watch
}

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

func (b *EtcdBackend) Unregister(name, addr string) error {
	_, err := b.Client.Delete(servicePath(name, addr), false)
	return err
}
