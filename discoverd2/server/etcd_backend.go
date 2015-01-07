package server

import (
	"encoding/json"
	"fmt"
	"log"
	"path"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
)

type etcdClient interface {
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
		done:     make(chan struct{}),
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
	return fmt.Sprintf("discoverd: %s %s not found", e.Service, e.Instance)
}

func (b *etcdBackend) instanceKey(service, id string) string {
	return path.Join(b.serviceKey(service), "instances", id)
}

func (b *etcdBackend) AddInstance(service string, inst *Instance) error {
	data, err := json.Marshal(inst)
	if err != nil {
		return err
	}
	_, err = b.etcd.Set(b.instanceKey(service, inst.ID), string(data), defaultTTL)
	return err
}

func (b *etcdBackend) RemoveInstance(service, id string) error {
	_, err := b.etcd.Delete(b.instanceKey(service, id), true)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
		return NotFoundError{Service: service, Instance: id}
	}
	return err
}

func (b *etcdBackend) Close() error {
	close(b.stopSync)
	<-b.done
	return nil
}

func (b *etcdBackend) StartSync() error {
	started := make(chan error)
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

					// ensure we have a key like /foo/bar/services/a/instances/id
					if strings.Count(res.Node.Key[len(keyPrefix):], "/") != 3 {
						continue
					}
					serviceName := strings.SplitN(res.Node.Key[len(keyPrefix)+1:], "/", 2)[0]
					instanceID := path.Base(res.Node.Key)

					if res.Action == "delete" {
						b.h.RemoveInstance(serviceName, instanceID)
					} else {
						inst := &Instance{}
						if err := json.Unmarshal([]byte(res.Node.Value), inst); err != nil {
							log.Printf("Error decoding JSON for instance %s: %s", res.Node.Key, err)
							continue
						}
						b.h.AddInstance(serviceName, inst)
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

		var instances []*Instance
		for _, n := range serviceNode.Nodes {
			if path.Base(n.Key) != "instances" {
				continue
			}
			for _, instNode := range n.Nodes {
				inst := &Instance{}
				if err := json.Unmarshal([]byte(instNode.Value), inst); err != nil {
					log.Printf("Error decoding JSON for instance %s: %s", instNode.Key, err)
					continue
				}
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
