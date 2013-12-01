package discover

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/flynn/go-etcd/etcd"
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
	response, err := b.getCurrentState(name)
	if err != nil {
		return nil, err
	}
	go func() {
		for _, u := range response.Kvs {
			if update := b.responseToUpdate(response, &u); update != nil {
				stream.ch <- update
			}
		}
		stream.ch <- &ServiceUpdate{}
		for u := range watch {
			if update := b.responseToUpdate(u, nil); update != nil {
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

func (b *EtcdBackend) responseToUpdate(resp *etcd.Response, kvp *etcd.KeyValuePair) *ServiceUpdate {
	var key, value string
	if kvp != nil {
		key = kvp.Key
		value = kvp.Value
	} else {
		key = resp.Key
		value = resp.Value
	}
	// expected key structure: /PREFIX/services/NAME/ADDR
	splitKey := strings.SplitN(key, "/", 5)
	if len(splitKey) < 5 {
		return nil
	}
	serviceName := splitKey[3]
	serviceAddr := splitKey[4]
	if "get" == resp.Action || ("set" == resp.Action && resp.Value != resp.PrevValue) {
		// GET is because getCurrentState returns responses of Action GET.
		// some SETs are heartbeats, so we ignore SETs where value didn't change.
		var serviceAttrs map[string]string
		err := json.Unmarshal([]byte(value), &serviceAttrs)
		if err != nil {
			return nil
		}
		return &ServiceUpdate{
			Name:   serviceName,
			Addr:   serviceAddr,
			Online: true,
			Attrs:  serviceAttrs,
		}
	} else if "delete" == resp.Action {
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
	attrsJson, err := json.Marshal(attrs)
	if err != nil {
		return err
	}
	_, err = b.Client.Set(servicePath(name, addr), string(attrsJson), HeartbeatIntervalSecs+MissedHearbeatTTL)
	return err
}

func (b *EtcdBackend) Heartbeat(name, addr string) error {
	resp, err := b.Client.Get(servicePath(name, addr), false, false)
	if err != nil {
		return err
	}
	// ignore test failure, it doesn't need a heartbeat if it was just set.
	_, err = b.Client.CompareAndSwap(servicePath(name, addr), resp.Value, HeartbeatIntervalSecs+MissedHearbeatTTL, resp.Value, resp.ModifiedIndex)
	return err
}

func (b *EtcdBackend) Unregister(name, addr string) error {
	_, err := b.Client.Delete(servicePath(name, addr), false)
	return err
}
