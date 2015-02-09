package main

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/router/types"
)

var ErrNotFound = errors.New("router: route not found")

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
