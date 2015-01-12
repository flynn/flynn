package server

import (
	"encoding/json"
	"fmt"
	"log"
	"path"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	"github.com/flynn/flynn/discoverd2/client"
)

type etcdClient interface {
	CreateDir(key string, ttl uint64) (*etcd.Response, error)
	Create(key string, value string, ttl uint64) (*etcd.Response, error)
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

type ServiceExistsError string

func (e ServiceExistsError) Error() string {
	return fmt.Sprintf("discoverd: service %q already exists", string(e))
}

func isEtcdError(err error, code int) bool {
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == code {
		return true
	}
	return false
}

func isEtcdNotFound(err error) bool { return isEtcdError(err, 100) }
func isEtcdExists(err error) bool   { return isEtcdError(err, 105) }

func (b *etcdBackend) instanceKey(service, id string) string {
	return path.Join(b.serviceKey(service), "instances", id)
}

func (b *etcdBackend) serviceKey(service string) string {
	return path.Join(b.prefix, "services", service)
}

func (b *etcdBackend) AddService(service string) error {
	_, err := b.etcd.CreateDir(b.serviceKey(service), 0)
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
	_, err = b.etcd.Set(b.instanceKey(service, inst.ID), string(data), defaultTTL)
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

func (b *etcdBackend) StartSync() error {
	started := make(chan error)
	b.done = make(chan struct{})
	go func() {
		defer close(b.done)
	outer:
		for {
			nextIndex, err := b.fullSync()
			if err != nil {
				if started != nil {
					started <- err
					return
				}
				log.Printf("Error while performing etcd fullsync: %s", err)
				// TODO: backoff/sleep
				continue
			}

			if started != nil {
				started <- nil
				started = nil
			}

			for {
				stream := make(chan *etcd.Response)
				watchDone := make(chan error)
				keyPrefix := path.Join(b.prefix, "services")

				go func() {
					_, err := b.etcd.Watch(keyPrefix, nextIndex, true, stream, b.stopSync)
					watchDone <- err
				}()

				for res := range stream {
					nextIndex = res.EtcdIndex + 1

					// ensure we have a key like /foo/bar/services/a/instances/id or /foo/bar/services/a
					slashes := strings.Count(res.Node.Key[len(keyPrefix):], "/")
					if slashes != 3 && slashes != 1 {
						continue
					}

					serviceName := strings.SplitN(res.Node.Key[len(keyPrefix)+1:], "/", 2)[0]
					if slashes == 3 {
						b.instanceEvent(serviceName, res)
					} else {
						b.serviceEvent(serviceName, res)
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
					continue outer
				}
				log.Printf("Restarting etcd watch %s due to error: %s", keyPrefix, watchErr)
				// TODO: add sleep/backoff
			}
		}
	}()
	return <-started
}

func (b *etcdBackend) instanceEvent(serviceName string, res *etcd.Response) {
	instanceID := path.Base(res.Node.Key)

	if res.Action == "delete" {
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
	} else {
		b.h.AddService(serviceName)
	}
}

func (b *etcdBackend) fullSync() (uint64, error) {
	keyPrefix := path.Join(b.prefix, "services")
	data, err := b.etcd.Get(keyPrefix, false, true)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
		// key not found, remove existing services
		for _, name := range b.h.ListServices() {
			b.h.SetService(name, nil)
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
		for _, n := range serviceNode.Nodes {
			if path.Base(n.Key) != "instances" {
				continue
			}
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
		b.h.SetService(serviceName, instances)
	}
	// remove any services that weren't found in the response
	for _, name := range b.h.ListServices() {
		if _, ok := added[name]; !ok {
			b.h.SetService(name, nil)
		}
	}

	return data.Node.ModifiedIndex, nil
}
