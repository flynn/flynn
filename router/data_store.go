package main

import (
	"errors"
	"fmt"
	"time"

	router "github.com/flynn/flynn/router/types"
	"github.com/jackc/pgx"
	"golang.org/x/net/context"
)

var ErrNotFound = errors.New("router: route not found")

type DataStore interface {
	Sync(ctx context.Context, h SyncHandler, startc chan<- struct{}) error
}

type SyncHandler interface {
	Set(route *router.Route) error
	Remove(id string) error
	Current() map[string]struct{}
}

type pgDataStore struct {
	pgx *pgx.ConnPool

	routeType string
	tableName string
}

const (
	routeTypeHTTP = "http"
	routeTypeTCP  = "tcp"
	tableNameHTTP = "http_routes"
	tableNameTCP  = "tcp_routes"
)

// NewPostgresDataStore returns a DataStore that uses pg_notify and a listener
// connection to watch for route changes.
func NewPostgresDataStore(routeType string, pgx *pgx.ConnPool) *pgDataStore {
	tableName := ""
	switch routeType {
	case routeTypeHTTP:
		tableName = tableNameHTTP
	case routeTypeTCP:
		tableName = tableNameTCP
	default:
		panic(fmt.Sprintf("unknown routeType: %q", routeType))
	}
	return &pgDataStore{
		pgx:       pgx,
		routeType: routeType,
		tableName: tableName,
	}
}

func (d *pgDataStore) Get(id string) (*router.Route, error) {
	if id == "" {
		return nil, ErrNotFound
	}

	var query string
	switch d.tableName {
	case tableNameHTTP:
		query = "http_route_select"
	case tableNameTCP:
		query = "tcp_route_select"
	}
	row := d.pgx.QueryRow(query, id)

	r := &router.Route{}
	err := d.scanRoute(r, row)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (d *pgDataStore) List() ([]*router.Route, error) {
	var query string
	switch d.tableName {
	case tableNameHTTP:
		query = "http_route_list"
	case tableNameTCP:
		query = "tcp_route_list"
	}
	rows, err := d.pgx.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []*router.Route{}
	for rows.Next() {
		r := &router.Route{}
		err := d.scanRoute(r, rows)
		if err != nil {
			return nil, err
		}

		r.Type = d.routeType
		routes = append(routes, r)
	}
	return routes, rows.Err()
}

func (d *pgDataStore) Sync(ctx context.Context, h SyncHandler, startc chan<- struct{}) error {
	ctx, cancel := context.WithCancel(ctx)

	idc, errc, err := d.startListener(ctx)
	if err != nil {
		return err
	}

	initialRoutes, err := d.List()
	if err != nil {
		cancel()
		return err
	}

	toRemove := h.Current()
	for _, route := range initialRoutes {
		if _, ok := toRemove[route.ID]; ok {
			delete(toRemove, route.ID)
		}
		if err := h.Set(route); err != nil {
			return err
		}
	}
	// send remove for any routes that are no longer in the database
	for id := range toRemove {
		if err := h.Remove(id); err != nil {
			return err
		}
	}
	close(startc)

	for {
		select {
		case id := <-idc:
			if err := d.handleUpdate(h, id); err != nil {
				cancel()
				return err
			}
		case err = <-errc:
			return err
		case <-ctx.Done():
			// wait for startListener to finish (it will either
			// close idc or send an error on errc)
			select {
			case <-idc:
			case <-errc:
			}
			return nil
		}
	}
}

func (d *pgDataStore) handleUpdate(h SyncHandler, id string) error {
	route, err := d.Get(id)
	if err == ErrNotFound {
		if err = h.Remove(id); err != nil && err != ErrNotFound {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}

	return h.Set(route)
}

func (d *pgDataStore) startListener(ctx context.Context) (<-chan string, <-chan error, error) {
	idc := make(chan string)
	errc := make(chan error)

	conn, err := d.pgx.Acquire()
	if err != nil {
		return nil, nil, err
	}
	if err = conn.Listen(d.tableName); err != nil {
		d.pgx.Release(conn)
		return nil, nil, err
	}

	go func() {
		defer unlistenAndRelease(d.pgx, conn, d.tableName)
		defer close(idc)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			notification, err := conn.WaitForNotification(time.Second)
			if err == pgx.ErrNotificationTimeout {
				continue
			}
			if err != nil {
				errc <- err
				return
			}

			idc <- notification.Payload
		}
	}()

	return idc, errc, nil
}

type scannable interface {
	Scan(dest ...interface{}) (err error)
}

func (d *pgDataStore) scanRoute(route *router.Route, s scannable) error {
	route.Type = d.routeType
	switch d.tableName {
	case tableNameHTTP:
		var certID, certCert, certKey *string
		var certCreatedAt, certUpdatedAt *time.Time
		if err := s.Scan(
			&route.ID,
			&route.ParentRef,
			&route.Service,
			&route.Port,
			&route.Leader,
			&route.DrainBackends,
			&route.Domain,
			&route.Sticky,
			&route.Path,
			&route.CreatedAt,
			&route.UpdatedAt,
			&certID,
			&certCert,
			&certKey,
			&certCreatedAt,
			&certUpdatedAt,
		); err != nil {
			return err
		}
		if certID != nil {
			route.Certificate = &router.Certificate{
				ID:        *certID,
				Cert:      *certCert,
				Key:       *certKey,
				CreatedAt: *certCreatedAt,
				UpdatedAt: *certUpdatedAt,
			}
		}
		return nil
	case tableNameTCP:
		return s.Scan(
			&route.ID,
			&route.ParentRef,
			&route.Service,
			&route.Port,
			&route.Leader,
			&route.DrainBackends,
			&route.CreatedAt,
			&route.UpdatedAt,
		)
	}
	panic("unknown tableName: " + d.tableName)
}

func unlistenAndRelease(pool *pgx.ConnPool, conn *pgx.Conn, channel string) {
	if err := conn.Unlisten(channel); err != nil {
		conn.Close()
	}
	pool.Release(conn)
}
