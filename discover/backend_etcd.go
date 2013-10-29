package discover

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
	responses, err := b.getCurrentState(name)
	if err != nil {
		return nil, err
	}
	go func() {
		for _, u := range responses {
			if update := b.responseToUpdate(u); update != nil {
				stream.ch <- update
			}
		}
		stream.ch <- &ServiceUpdate{}
		for u := range watch {
			if update := b.responseToUpdate(u); update != nil {
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

func (b *EtcdBackend) responseToUpdate(resp *etcd.Response) *ServiceUpdate {
	// expected key structure: /PREFIX/services/NAME/ADDR
	splitKey := strings.SplitN(resp.Key, "/", 5)
	if len(splitKey) < 5 {
		return nil
	}
	serviceName := splitKey[3]
	serviceAddr := splitKey[4]
	if "GET" == resp.Action || ("SET" == resp.Action && (resp.NewKey || resp.Value != resp.PrevValue)) {
		// GET is because getCurrentState returns responses of Action GET.
		// some SETs are heartbeats, so we ignore SETs where value didn't change.
		var serviceAttrs map[string]string
		err := json.Unmarshal([]byte(resp.Value), &serviceAttrs)
		if err != nil {
			return nil
		}
		return &ServiceUpdate{
			Name:   serviceName,
			Addr:   serviceAddr,
			Online: true,
			Attrs:  serviceAttrs,
		}
	} else if "DELETE" == resp.Action {
		return &ServiceUpdate{
			Name: serviceName,
			Addr: serviceAddr,
		}
	} else {
		return nil
	}
}

func (b *EtcdBackend) getCurrentState(name string) ([]*etcd.Response, error) {
	return b.Client.Get(servicePath(name, ""))
}

func (b *EtcdBackend) getStateChanges(name string, stop chan bool) chan *etcd.Response {
	watch := make(chan *etcd.Response)
	go b.Client.Watch(servicePath(name, ""), 0, watch, stop)
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
	resp, err1 := b.Client.Get(servicePath(name, addr))
	if err1 != nil {
		return err1
	}
	// ignore test failure, it doesn't need a heartbeat if it was just set.
	_, _, err2 := b.Client.TestAndSet(servicePath(name, addr), resp[0].Value, resp[0].Value, HeartbeatIntervalSecs+MissedHearbeatTTL)
	return err2
}

func (b *EtcdBackend) Unregister(name, addr string) error {
	_, err := b.Client.Delete(servicePath(name, addr))
	return err
}
