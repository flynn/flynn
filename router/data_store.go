package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/coreos/go-etcd/etcd"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/router/types"
)

type EtcdClient interface {
	Create(key string, value string, ttl uint64) (*etcd.Response, error)
	Set(key string, value string, ttl uint64) (*etcd.Response, error)
	Get(key string, sort, recursive bool) (*etcd.Response, error)
	Delete(key string, recursive bool) (*etcd.Response, error)
	Watch(prefix string, waitIndex uint64, recursive bool, receiver chan *etcd.Response, stop chan bool) (*etcd.Response, error)
}

type DataStore interface {
	Add(route *router.Route) error
	Set(route *router.Route) error
	Get(id string) (*router.Route, error)
	List() ([]*router.Route, error)
	Remove(id string) error
	Sync(h SyncHandler, started chan<- error)
	StopSync()
}

type DataStoreReader interface {
	Get(id string) (*router.Route, error)
	List() ([]*router.Route, error)
}

type SyncHandler interface {
	Set(route *router.Route) error
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

var ErrExists = errors.New("router: route already exists")
var ErrNotFound = errors.New("router: route not found")

func (s *etcdDataStore) Add(r *router.Route) error {
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

func (s *etcdDataStore) Set(r *router.Route) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.etcd.Set(s.path(r.ID), string(data), 0)
	return err
}

func (s *etcdDataStore) Remove(id string) error {
	_, err := s.etcd.Delete(s.path(id), true)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
		return ErrNotFound
	}
	return err
}

func (s *etcdDataStore) Get(id string) (*router.Route, error) {
	res, err := s.etcd.Get(s.path(id), false, false)
	if err != nil {
		if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
			err = ErrNotFound
		}
		return nil, err
	}
	r := &router.Route{}
	err = json.Unmarshal([]byte(res.Node.Value), r)
	return r, err
}

func (s *etcdDataStore) List() ([]*router.Route, error) {
	res, err := s.etcd.Get(s.prefix, false, true)
	if err != nil {
		if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
			err = nil
		}
		return nil, err
	}

	routes := make([]*router.Route, len(res.Node.Nodes))
	for i, node := range res.Node.Nodes {
		r := &router.Route{}
		if err := json.Unmarshal([]byte(node.Value), r); err != nil {
			return nil, err
		}
		routes[i] = r
	}
	return routes, nil
}

func (s *etcdDataStore) path(id string) string {
	return path.Join(s.prefix, path.Base(id))
}

func (s *etcdDataStore) Sync(h SyncHandler, started chan<- error) {
	keys := make(map[string]uint64)
	newKeys := make(map[string]uint64)
	nextIndex := uint64(1)

fullSync:
	data, err := s.etcd.Get(s.prefix, false, true)
	if e, ok := err.(*etcd.EtcdError); ok && e.ErrorCode == 100 {
		nextIndex = e.Index + 1
		// key not found, delete existing keys and then watch
		for id := range keys {
			delete(keys, id)
			if err := h.Remove(id); err != nil {
				log.Printf("Error while processing delete from etcd fullsync: %s, %s", id, err)
			}
		}
		goto watch
	}

syncErr:
	if err != nil {
		if started != nil {
			started <- err
			return
		}
		log.Printf("Error while doing fullsync from etcd: %s", err)
		time.Sleep(time.Second)
		goto fullSync
	}

	nextIndex = data.EtcdIndex + 1
	for _, node := range data.Node.Nodes {
		key := path.Base(node.Key)
		if modified, ok := keys[key]; ok && modified >= node.ModifiedIndex {
			newKeys[key] = modified
			continue
		}
		route := &router.Route{}
		if err = json.Unmarshal([]byte(node.Value), route); err != nil {
			goto syncErr
		}
		if err = h.Set(route); err != nil {
			goto syncErr
		}
		newKeys[key] = node.ModifiedIndex
	}
	for k := range keys {
		if _, ok := newKeys[k]; ok {
			continue
		}
		if err := h.Remove(k); err != nil {
			log.Printf("Error while processing delete from etcd fullsync: %s, %s", k, err)
		}
	}
	keys = newKeys
	newKeys = make(map[string]uint64)

watch:
	if started != nil {
		started <- nil
		started = nil
	}

	for {
		stream := make(chan *etcd.Response)
		watchDone := make(chan struct{})
		var watchErr error
		go func() {
			_, watchErr = s.etcd.Watch(s.prefix, nextIndex, true, stream, s.stopSync)
			close(watchDone)
		}()
		for res := range stream {
			nextIndex = res.EtcdIndex + 1
			id := path.Base(res.Node.Key)
			var err error
			if res.Action == "delete" {
				delete(keys, id)
				err = h.Remove(id)
			} else {
				keys[id] = res.Node.ModifiedIndex
				route := &router.Route{}
				if err = json.Unmarshal([]byte(res.Node.Value), route); err != nil {
					goto fail
				}
				err = h.Set(route)
			}
		fail:
			if err != nil {
				log.Printf("Error while processing update from etcd: %s, %#v", err, res.Node)
			}
		}
		<-watchDone
		select {
		case <-s.stopSync:
			return
		default:
		}
		if e, ok := watchErr.(*etcd.EtcdError); ok && e.ErrorCode == 401 {
			// event log has been pruned beyond our waitIndex, force fullSync
			log.Printf("Got etcd error 401, doing full sync")
			goto fullSync
		}
		log.Printf("Restarting etcd watch %s due to error: %s", s.prefix, watchErr)
		time.Sleep(time.Second)
	}
}

func (s *etcdDataStore) StopSync() {
	close(s.stopSync)
}

type pgDataStore struct {
	pgx *pgx.ConnPool

	rt string

	doneo sync.Once
	donec chan struct{}
}

// NewPostgresDataStore returns a DataStore that stores route information in a
// Postgres database. It uses pg_notify and a listener connection to watch for
// route changes.
func NewPostgresDataStore(routeType string, pgx *pgx.ConnPool) *pgDataStore {
	return &pgDataStore{
		pgx:   pgx,
		rt:    routeType,
		donec: make(chan struct{}),
	}
}

const sqlAddRoute = `INSERT INTO routes (parent_ref, type, config) VALUES ($1, $2, $3::json) RETURNING route_id, created_at, updated_at`

func (d *pgDataStore) Add(r *router.Route) error {
	if r.CreatedAt == nil {
		r.CreatedAt = &time.Time{}
	}
	if r.UpdatedAt == nil {
		r.UpdatedAt = &time.Time{}
	}
	return d.pgx.QueryRow(sqlAddRoute, r.ParentRef, r.Type, r.Config).Scan(&r.ID, r.CreatedAt, r.UpdatedAt)
}

const sqlSetRoute = `UPDATE routes SET parent_ref = $1, type = $2, config = $3::json WHERE route_id = $4 RETURNING updated_at`

func (d *pgDataStore) Set(r *router.Route) error {
	if r.UpdatedAt == nil {
		r.UpdatedAt = &time.Time{}
	}
	err := d.pgx.QueryRow(sqlSetRoute, r.ParentRef, r.Type, r.Config, r.ID).Scan(r.UpdatedAt)
	if err == pgx.ErrNoRows {
		return ErrNotFound
	}
	return err
}

const sqlRemoveRoute = `UPDATE routes SET deleted_at = now() WHERE route_id = $1`

func (d *pgDataStore) Remove(id string) error {
	_, err := d.pgx.Exec(sqlRemoveRoute, id)
	return err
}

const sqlGetRoute = `SELECT parent_ref, type, config, created_at, updated_at FROM routes WHERE route_id = $1 AND type = $2 AND deleted_at IS NULL`

func (d *pgDataStore) Get(id string) (*router.Route, error) {
	r := newRoute()
	row := d.pgx.QueryRow(sqlGetRoute, id, d.rt)

	err := row.Scan(&r.ParentRef, &r.Type, r.Config, r.CreatedAt, r.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	r.ID = id
	return r, nil
}

const sqlListRoutes = `SELECT route_id, parent_ref, type, config, created_at, updated_at FROM routes WHERE type = $1 AND deleted_at IS NULL`

func (d *pgDataStore) List() ([]*router.Route, error) {
	rows, err := d.pgx.Query(sqlListRoutes, d.rt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []*router.Route{}
	for rows.Next() {
		r := newRoute()
		err := rows.Scan(&r.ID, &r.ParentRef, &r.Type, r.Config, r.CreatedAt, r.UpdatedAt)
		if err != nil {
			return nil, err
		}

		routes = append(routes, r)
	}
	return routes, rows.Err()
}

func (d *pgDataStore) Sync(h SyncHandler, started chan<- error) {
	idc := make(chan string)
	if err := d.startListener(idc); err != nil {
		started <- err
		return
	}
	defer d.StopSync()

	initialRoutes, err := d.List()
	if err != nil {
		started <- err
		return
	}

	for _, route := range initialRoutes {
		if err := h.Set(route); err != nil {
			started <- err
			return
		}
	}
	close(started)

	for {
		select {
		case id := <-idc:
			d.handleUpdate(h, id)
		case <-d.donec:
			for range idc {
			}
			return
		}
	}
}

func (d *pgDataStore) StopSync() {
	d.doneo.Do(func() { close(d.donec) })
}

func (d *pgDataStore) handleUpdate(h SyncHandler, id string) {
	route, err := d.Get(id)
	if err == ErrNotFound {
		if err = h.Remove(id); err != nil && err != ErrNotFound {
			// TODO(benburkert): structured logging
			log.Printf("router: sync handler remove error: %s, %s", id, err)
		}
		return
	}

	if err != nil {
		log.Printf("router: datastore error: %s, %s", id, err)
		return
	}

	if err := h.Set(route); err != nil {
		log.Printf("router: sync handler set error: %s, %s", id, err)
	}
}

func (d *pgDataStore) startListener(idc chan<- string) error {
	conn, err := d.pgx.Acquire()
	if err != nil {
		return err
	}
	if err = conn.Listen("routes"); err != nil {
		d.pgx.Release(conn)
		return err
	}

	go func() {
		defer unlistenAndRelease(d.pgx, conn, "routes")
		defer close(idc)

		for {
			select {
			case <-d.donec:
				return
			default:
			}
			notification, err := conn.WaitForNotification(time.Second)
			if err == pgx.ErrNotificationTimeout {
				continue
			}
			if err != nil {
				log.Printf("router: notifier error: %s", err)
				d.StopSync()
				return
			}

			data := strings.Split(notification.Payload, ":")
			if len(data) != 2 {
				log.Printf("router: invalid route notification: %s", notification.Payload)
				defer d.StopSync()
				return
			}

			routeType, id := data[0], data[1]
			if routeType == d.rt {
				idc <- id
			}
		}
	}()

	return nil
}

const sqlUnlisten = `UNLISTEN %s`

func unlistenAndRelease(pool *pgx.ConnPool, conn *pgx.Conn, channel string) {
	_, err := conn.Exec(fmt.Sprintf(sqlUnlisten, channel))
	if err != nil {
		conn.Close()
		return
	}
	pool.Release(conn)
}

func newRoute() *router.Route {
	return &router.Route{
		Config:    &router.Config{},
		CreatedAt: &time.Time{},
		UpdatedAt: &time.Time{},
	}
}
