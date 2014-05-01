package main

import (
	"encoding/json"
	"errors"
	"log"
	"path"

	"github.com/coreos/go-etcd/etcd"
	"github.com/flynn/strowger/types"
)

type EtcdClient interface {
	Create(key string, value string, ttl uint64) (*etcd.Response, error)
	Get(key string, sort, recursive bool) (*etcd.Response, error)
	Delete(key string, recursive bool) (*etcd.Response, error)
	Watch(prefix string, waitIndex uint64, recursive bool, receiver chan *etcd.Response, stop chan bool) (*etcd.Response, error)
}

type DataStore interface {
	Add(route *strowger.Route) error
	Get(id string) (*strowger.Route, error)
	List() ([]*strowger.Route, error)
	Remove(id string) error
	Sync(h SyncHandler, started chan<- error)
	StopSync()
}

type DataStoreReader interface {
	Get(id string) (*strowger.Route, error)
	List() ([]*strowger.Route, error)
}

type SyncHandler interface {
	Add(route *strowger.Route) error
	Remove(id string) error
}

func NewEtcdDataStore(etcd EtcdClient, prefix string) DataStore {
	return &etcdDataStore{
		prefix:   prefix,
		etcd:     etcd,
		stopSync: make(chan bool),
	}
}

type etcdDataStore struct {
	prefix   string
	etcd     EtcdClient
	stopSync chan bool
}

var ErrExists = errors.New("strowger: route already exists")
var ErrNotFound = errors.New("strowger: route not found")

func (s *etcdDataStore) Add(r *strowger.Route) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.etcd.Create(s.path(r.ID), string(data), 0)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 105 {
		err = ErrExists
	}
	return err
}

func (s *etcdDataStore) Remove(id string) error {
	_, err := s.etcd.Delete(s.path(id), true)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
		return ErrNotFound
	}
	return err
}

func (s *etcdDataStore) Get(id string) (*strowger.Route, error) {
	res, err := s.etcd.Get(s.path(id), false, false)
	if err != nil {
		if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
			err = ErrNotFound
		}
		return nil, err
	}
	r := &strowger.Route{}
	err = json.Unmarshal([]byte(res.Node.Value), r)
	return r, err
}

func (s *etcdDataStore) List() ([]*strowger.Route, error) {
	res, err := s.etcd.Get(s.prefix, false, true)
	if err != nil {
		if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
			err = nil
		}
		return nil, err
	}

	routes := make([]*strowger.Route, len(res.Node.Nodes))
	for i, node := range res.Node.Nodes {
		r := &strowger.Route{}
		if err := json.Unmarshal([]byte(node.Value), r); err != nil {
			return nil, err
		}
		routes[i] = r
	}
	return routes, nil
}

func (s *etcdDataStore) path(id string) string {
	return s.prefix + path.Base(id)
}

func (s *etcdDataStore) Sync(h SyncHandler, started chan<- error) {
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
		route := &strowger.Route{}
		if err := json.Unmarshal([]byte(node.Value), route); err != nil {
			started <- err
			return
		}
		if err := h.Add(route); err != nil {
			started <- err
			return
		}
	}

watch:
	started <- nil
	stream := make(chan *etcd.Response)
	go s.etcd.Watch(s.prefix, since, true, stream, s.stopSync)
	for res := range stream {
		id := path.Base(res.Node.Key)
		var err error
		if res.Action == "delete" {
			err = h.Remove(id)
		} else {
			route := &strowger.Route{}
			if err = json.Unmarshal([]byte(res.Node.Value), route); err != nil {
				goto fail
			}
			err = h.Add(route)
		}
	fail:
		if err != nil {
			log.Printf("Error while processing update from etcd: %s, %#v", err, res.Node)
		}
	}
}

func (s *etcdDataStore) StopSync() {
	close(s.stopSync)
}
