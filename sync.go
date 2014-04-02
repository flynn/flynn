package main

import (
	"fmt"
	"path"

	"github.com/flynn/go-etcd/etcd"
)

type SyncHandler interface {
	Add(data []byte) error
	Remove(id string) error
}

type EtcdClient interface {
	Create(key string, value string, ttl uint64) (*etcd.Response, error)
	Get(key string, sort, recursive bool) (*etcd.Response, error)
	Delete(key string, recursive bool) (*etcd.Response, error)
	Watch(prefix string, waitIndex uint64, recursive bool, receiver chan *etcd.Response, stop chan bool) (*etcd.Response, error)
}

func NewEtcdSynchronizer(etcd EtcdClient, prefix string, handler SyncHandler) Synchronizer {
	return &etcdSynchronizer{
		etcd:   etcd,
		prefix: prefix,
		h:      handler,
		stop:   make(chan bool),
	}
}

type Synchronizer interface {
	Sync(started chan<- error)
	Stop()
}

type etcdSynchronizer struct {
	etcd   EtcdClient
	h      SyncHandler
	prefix string
	stop   chan bool
}

func (s *etcdSynchronizer) Sync(started chan<- error) {
	var since uint64
	data, err := s.etcd.Get(s.prefix, false, true)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
		// key not found, ignore
		goto watch
	}
	if err != nil {
		started <- err
		return
	}
	since = data.EtcdIndex
	for _, node := range data.Node.Nodes {
		if err := s.h.Add([]byte(node.Value)); err != nil {
			started <- err
			return
		}
	}

watch:
	started <- nil
	stream := make(chan *etcd.Response)
	go s.etcd.Watch(s.prefix, since, true, stream, s.stop)
	for res := range stream {
		id := path.Base(res.Node.Key)
		var err error
		if res.Action == "delete" {
			err = s.h.Remove(id)
		} else {
			err = s.h.Add([]byte(res.Node.Value))
		}
		if err != nil {
			panic(fmt.Sprintf("Error while processing update from etcd: %s", err))
		}
	}
}

func (s *etcdSynchronizer) Stop() {
	close(s.stop)
}
