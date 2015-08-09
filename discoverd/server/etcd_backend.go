package server

import (
	"encoding/json"
	"fmt"
	"log"
	"path"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	"github.com/flynn/flynn/discoverd/client"
	hh "github.com/flynn/flynn/pkg/httphelper"
)

type etcdClient interface {
	CreateDir(key string, ttl uint64) (*etcd.Response, error)
	Create(key string, value string, ttl uint64) (*etcd.Response, error)
	Update(key string, value string, ttl uint64) (*etcd.Response, error)
	CompareAndSwap(key string, value string, ttl uint64, prevValue string, prevIndex uint64) (*etcd.Response, error)
	Set(key string, value string, ttl uint64) (*etcd.Response, error)
	Get(key string, sort, recursive bool) (*etcd.Response, error)
	Delete(key string, recursive bool) (*etcd.Response, error)
	Watch(prefix string, waitIndex uint64, recursive bool, receiver chan *etcd.Response, stop chan bool) (*etcd.Response, error)
}

func NewEtcdBackend(client etcdClient, prefix string, h SyncHandler) Backend {
	return &etcdBackend{
		prefix:   prefix,
		etcd:     client,
		h:        h,
		stopSync: make(chan bool),
	}
}

type etcdBackend struct {
	prefix   string
	etcd     etcdClient
	h        SyncHandler
	stopSync chan bool
	done     chan struct{}
}

const defaultTTL = 10

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

func isEtcdError(err error, code int) bool {
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == code {
		return true
	}
	return false
}

func isEtcdNotFound(err error) bool      { return isEtcdError(err, 100) }
func isEtcdCompareFailed(err error) bool { return isEtcdError(err, 101) }
func isEtcdExists(err error) bool        { return isEtcdError(err, 105) }

func (b *etcdBackend) instanceKey(service, id string) string {
	return path.Join(b.serviceKey(service), "instances", id)
}

func (b *etcdBackend) serviceKey(service string) string {
	return path.Join(b.prefix, "services", service)
}

func (b *etcdBackend) Ping() error {
	_, err := b.etcd.Get("/ping-nonexistent", false, false)
	if isEtcdNotFound(err) {
		err = nil
	}
	return err
}

func (b *etcdBackend) AddService(service string, config *discoverd.ServiceConfig) error {
	if config == nil {
		config = DefaultServiceConfig
	}
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	_, err = b.etcd.Create(path.Join(b.serviceKey(service), "config"), string(data), 0)
	if isEtcdExists(err) {
		return ServiceExistsError(service)
	}
	return err
}

func (b *etcdBackend) RemoveService(service string) error {
	_, err := b.etcd.Delete(b.serviceKey(service), true)
	if isEtcdNotFound(err) {
		return NotFoundError{Service: service}
	}
	return err
}

func (b *etcdBackend) SetServiceMeta(service string, meta *discoverd.ServiceMeta) error {
	serviceKey := b.serviceKey(service)
	_, err := b.etcd.Get(serviceKey, false, false)
	if isEtcdNotFound(err) {
		return NotFoundError{Service: service}
	}
	if err != nil {
		return err
	}

	var res *etcd.Response
	key := path.Join(serviceKey, "meta")
	if meta.Index == 0 {
		res, err = b.etcd.Create(key, string(meta.Data), 0)
		if isEtcdExists(err) {
			err = hh.ObjectExistsErr(fmt.Sprintf("Service metadata for %q already exists, use index=n to set", service))
		}
	} else {
		res, err = b.etcd.CompareAndSwap(key, string(meta.Data), 0, "", meta.Index)
		if isEtcdNotFound(err) {
			err = hh.PreconditionFailedErr(fmt.Sprintf("Service metadata for %q does not exist, use index=0 to set", service))
		} else if isEtcdCompareFailed(err) {
			err = hh.PreconditionFailedErr(fmt.Sprintf("Service metadata for %q exists, but wrong index provided", service))
		}
	}
	if err != nil {
		return err
	}
	meta.Index = res.Node.ModifiedIndex
	return nil
}

func (b *etcdBackend) SetLeader(service, id string) error {
	_, err := b.etcd.Set(path.Join(b.serviceKey(service), "leader"), id, 0)
	return err
}

func (b *etcdBackend) AddInstance(service string, inst *discoverd.Instance) error {
	data, err := json.Marshal(inst)
	if err != nil {
		return err
	}
	_, err = b.etcd.Get(b.serviceKey(service), false, false)
	if isEtcdNotFound(err) {
		return NotFoundError{Service: service}
	}
	if err != nil {
		return err
	}
	dataString := string(data)
	key := b.instanceKey(service, inst.ID)

	_, err = b.etcd.Update(key, dataString, defaultTTL)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
		// This is a workaround for etcd issue #407: https://github.com/coreos/etcd/issues/407
		// If we just do a Set and don't try to Update first, createdIndex will get incremented
		// on each heartbeat, breaking leader election.
		_, err = b.etcd.Set(key, dataString, defaultTTL)
	}

	return err
}

func (b *etcdBackend) RemoveInstance(service, id string) error {
	_, err := b.etcd.Delete(b.instanceKey(service, id), true)
	if isEtcdNotFound(err) {
		return NotFoundError{Service: service, Instance: id}
	}
	return err
}

func (b *etcdBackend) Close() error {
	if b.done != nil {
		close(b.stopSync)
		<-b.done
		b.done = nil
	}
	return nil
}

const retrySleep = 1 * time.Second

func (b *etcdBackend) StartSync() error {
	started := make(chan error)
	b.done = make(chan struct{})
	go func() {
		defer close(b.done)
		recentError := false
		maybeRetrySleep := func() {
			if recentError {
				time.Sleep(retrySleep)
			}
			recentError = true
		}
	outer:
		for {
			nextIndex, err := b.fullSync()
			if err != nil {
				if started != nil {
					started <- err
					return
				}
				log.Printf("Error while performing etcd fullsync: %s", err)
				maybeRetrySleep()
				continue
			}

			if started != nil {
				started <- nil
				started = nil
			}
			recentError = false

			for {
				stream := make(chan *etcd.Response)
				watchDone := make(chan error)
				keyPrefix := path.Join(b.prefix, "services")

				go func() {
					_, err := b.etcd.Watch(keyPrefix, nextIndex, true, stream, b.stopSync)
					watchDone <- err
				}()

				for res := range stream {
					recentError = false
					nextIndex = res.EtcdIndex + 1

					// ensure we have a key like:
					// /foo/bar/services/a/instances/id,
					// /foo/bar/services/a/meta, etc
					slashes := strings.Count(res.Node.Key[len(keyPrefix):], "/")
					if slashes < 1 || slashes > 3 {
						continue
					}

					serviceName := strings.SplitN(res.Node.Key[len(keyPrefix)+1:], "/", 2)[0]
					switch slashes {
					case 1:
						b.serviceEvent(serviceName, res)
					case 2:
						switch path.Base(res.Node.Key) {
						case "meta":
							b.h.SetServiceMeta(serviceName, []byte(res.Node.Value), res.Node.ModifiedIndex)
						case "config":
							b.serviceEvent(serviceName, res)
						case "leader":
							b.h.SetLeader(serviceName, res.Node.Value)
						}
					case 3:
						b.instanceEvent(serviceName, res)
					}
				}

				watchErr := <-watchDone
				select {
				case <-b.stopSync:
					return
				default:
				}

				if e, ok := watchErr.(*etcd.EtcdError); ok && e.ErrorCode == 401 {
					// event log has been pruned beyond our waitIndex, force fullSync
					log.Printf("Got etcd error 401, doing full sync")
					maybeRetrySleep()
					continue outer
				}
				log.Printf("Restarting etcd watch %s due to error: %s", keyPrefix, watchErr)
				maybeRetrySleep()
			}
		}
	}()
	return <-started
}

func (b *etcdBackend) instanceEvent(serviceName string, res *etcd.Response) {
	instanceID := path.Base(res.Node.Key)

	if res.Action == "delete" || res.Action == "expire" {
		b.h.RemoveInstance(serviceName, instanceID)
	} else {
		inst := &discoverd.Instance{}
		if err := json.Unmarshal([]byte(res.Node.Value), inst); err != nil {
			log.Printf("Error decoding JSON for instance %s: %s", res.Node.Key, err)
			return
		}
		inst.Index = res.Node.CreatedIndex
		b.h.AddInstance(serviceName, inst)
	}
}

func (b *etcdBackend) serviceEvent(serviceName string, res *etcd.Response) {
	if res.Action == "delete" {
		b.h.RemoveService(serviceName)
		return
	}
	config := &discoverd.ServiceConfig{}
	if err := json.Unmarshal([]byte(res.Node.Value), config); err != nil {
		log.Printf("Error decoding JSON for service config %s: %s", res.Node.Key, err)
		return
	}
	b.h.AddService(serviceName, config)
}

func (b *etcdBackend) fullSync() (uint64, error) {
	keyPrefix := path.Join(b.prefix, "services")
	data, err := b.etcd.Get(keyPrefix, false, true)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
		// key not found, remove existing services
		for _, name := range b.h.ListServices() {
			b.h.SetService(name, nil, nil)
		}
		return e.Index + 1, nil
	}
	if err != nil {
		return 1, err
	}

	added := make(map[string]struct{}, len(data.Node.Nodes))
	for _, serviceNode := range data.Node.Nodes {
		serviceName := path.Base(serviceNode.Key)
		added[serviceName] = struct{}{}

		instances := []*discoverd.Instance{}
		var config *discoverd.ServiceConfig
		var leaderID string
		for _, n := range serviceNode.Nodes {
			switch path.Base(n.Key) {
			case "meta":
				b.h.SetServiceMeta(serviceName, []byte(n.Value), n.ModifiedIndex)
			case "config":
				config = &discoverd.ServiceConfig{}
				if err := json.Unmarshal([]byte(n.Value), config); err != nil {
					log.Printf("Error decoding JSON config for service %s: %s", n.Key, err)
				}
			case "leader":
				leaderID = n.Value
			case "instances":
				for _, instNode := range n.Nodes {
					inst := &discoverd.Instance{}
					if err := json.Unmarshal([]byte(instNode.Value), inst); err != nil {
						log.Printf("Error decoding JSON for instance %s: %s", instNode.Key, err)
						continue
					}
					inst.Index = instNode.CreatedIndex
					instances = append(instances, inst)
				}
			}
		}
		if config == nil {
			config = DefaultServiceConfig
		}
		b.h.SetService(serviceName, config, instances)
		if leaderID != "" {
			b.h.SetLeader(serviceName, leaderID)
		}
	}
	// remove any services that weren't found in the response
	for _, name := range b.h.ListServices() {
		if _, ok := added[name]; !ok {
			b.h.SetService(name, nil, nil)
		}
	}

	return data.EtcdIndex + 1, nil
}
