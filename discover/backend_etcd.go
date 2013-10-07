package discover

import (
	"fmt"
	"strings"
	"sync"

	"github.com/coreos/etcd/store"
	"github.com/coreos/go-etcd/etcd"
)

type EtcdBackend struct {
	Client *etcd.Client
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

func (b *EtcdBackend) responseToUpdate(resp *store.Response) *ServiceUpdate {
	respList := strings.SplitN(resp.Key, "/", 4)
	if len(respList) < 3 {
		return nil
	}

	serviceName := respList[2]
	if ("SET" == resp.Action && resp.NewKey) || "GET" == resp.Action {
		return &ServiceUpdate{
			Name:   serviceName,
			Addr:   resp.Value,
			Online: true,
		}
	} else if "DELETE" == resp.Action {
		return &ServiceUpdate{
			Name: serviceName,
			Addr: resp.PrevValue,
		}
	} else {
		return nil
	}
}

func (b *EtcdBackend) getCurrentState(name string) ([]*store.Response, error) {
	return b.Client.Get(fmt.Sprintf("/services/%s", name))
}

func (b *EtcdBackend) getStateChanges(name string, stop chan bool) chan *store.Response {
	watch := make(chan *store.Response)
	go b.Client.Watch(fmt.Sprintf("/services/%s", name), 0, watch, stop)
	return watch
}

func (b *EtcdBackend) Register(name string, addr string, attrs map[string]string) error {
	_, err := b.Client.Set(fmt.Sprintf("/services/%s/%s", name, addr), addr, HeartbeatIntervalSecs+MissedHearbeatTTL)
	return err
}

func (b *EtcdBackend) Unregister(name string, addr string) error {
	_, err := b.Client.Delete(fmt.Sprintf("/services/%s/%s", name, addr))
	return err
}

func (b *EtcdBackend) Heartbeat(name string, addr string) error {
	// Heartbeat currently just calls Register because eventually Register will also update attributes
	// where Heartbeat will not
	return b.Register(name, addr, map[string]string{})
}
