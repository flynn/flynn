package main

import (
	"encoding/json"
	"errors"
	"log"
	"path"

	"github.com/flynn/go-etcd/etcd"
)

type EtcdClient interface {
	Create(key string, value string, ttl uint64) (*etcd.Response, error)
	Get(key string, sort, recursive bool) (*etcd.Response, error)
	Delete(key string, recursive bool) (*etcd.Response, error)
	Watch(prefix string, waitIndex uint64, recursive bool, receiver chan *etcd.Response, stop chan bool) (*etcd.Response, error)
}

type DataStore interface {
	Add(id string, data interface{}) error
	Remove(id string) error
	Sync(h SyncHandler, started chan<- error)
	StopSync()
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

func (s *etcdDataStore) Add(id string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.etcd.Create(s.prefix+id, string(data), 0)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 105 {
		err = ErrExists
	}
	return err
}

func (s *etcdDataStore) Remove(id string) error {
	_, err := s.etcd.Delete(s.prefix+id, true)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
		return ErrNotFound
	}
	return err
}

type SyncHandler interface {
	Add(data []byte) error
	Remove(id string) error
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
		if err := h.Add([]byte(node.Value)); err != nil {
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
			err = h.Add([]byte(res.Node.Value))
		}
		if err != nil {
			log.Printf("Error while processing update from etcd: %s, %#v", err, res.Node)
		}
	}
}

func (s *etcdDataStore) StopSync() {
	close(s.stopSync)
}
